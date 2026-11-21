package analysis

// events.go 实现进程内 Agent 事件中心，为 SSE 提供有界历史缓冲、
// 多订阅者广播和断线后的历史事件回放。
import (
	"context"
	"sync"
	"sync/atomic"
	"time"
)

// maxRunEventHistory 是每个分析任务在内存中保留的最大事件数量。
const maxRunEventHistory = 512

// EventHub 管理分析任务事件的进程内广播和有界历史回放。
type EventHub struct {
	mu sync.RWMutex
	// history 按任务 ID 保存最近事件，供订阅者补发历史。
	history map[string][]Event
	// subscribers 保存每个任务的活跃 SSE 订阅通道。
	subscribers map[string]map[chan Event]struct{}
	// sequence 为所有事件分配全局单调递增序号。
	sequence atomic.Int64
}

// NewEventHub 创建空的事件中心。
func NewEventHub() *EventHub {
	return &EventHub{
		history:     make(map[string][]Event),
		subscribers: make(map[string]map[chan Event]struct{}),
	}
}

// Publish 发布事件、追加有界历史，并尽力广播给所有订阅者。
func (h *EventHub) Publish(runID, eventType string, data any) Event {
	event := Event{
		RunID:     runID,
		Type:      eventType,
		Sequence:  h.sequence.Add(1),
		Timestamp: time.Now(),
		Data:      data,
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	history := append(h.history[runID], event)
	if len(history) > maxRunEventHistory {
		history = history[len(history)-maxRunEventHistory:]
	}
	h.history[runID] = history

	for subscriber := range h.subscribers[runID] {
		select {
		case subscriber <- event:
		default:
			// A slow browser must not block agent execution.
		}
	}
	return event
}

// Subscribe 返回指定任务的实时事件通道以及已产生的历史事件。
// 订阅会在 ctx 取消后自动注销。
func (h *EventHub) Subscribe(
	ctx context.Context,
	runID string,
) (<-chan Event, []Event) {
	channel := make(chan Event, 64)

	h.mu.Lock()
	history := append([]Event(nil), h.history[runID]...)
	if h.subscribers[runID] == nil {
		h.subscribers[runID] = make(map[chan Event]struct{})
	}
	h.subscribers[runID][channel] = struct{}{}
	h.mu.Unlock()

	go func() {
		<-ctx.Done()
		h.mu.Lock()
		delete(h.subscribers[runID], channel)
		if len(h.subscribers[runID]) == 0 {
			delete(h.subscribers, runID)
		}
		close(channel)
		h.mu.Unlock()
	}()

	return channel, history
}

// Clear 删除指定任务的缓存事件。
func (h *EventHub) Clear(runID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.history, runID)
}

// eventsFromDetail 从持久化详情重建可回放的关键事件。
func eventsFromDetail(detail *RunDetail) []Event {
	if detail == nil {
		return nil
	}
	events := make([]Event, 0, 2+len(detail.Steps)+len(detail.ToolCalls)+len(detail.Artifacts))
	events = append(events, Event{
		RunID:     detail.Run.ID,
		Type:      EventRunStarted,
		Timestamp: detail.Run.CreatedAt,
		Data:      detail.Run,
	})
	if len(detail.Run.Plan) > 0 {
		events = append(events, Event{
			RunID:     detail.Run.ID,
			Type:      EventPlanCreated,
			Timestamp: detail.Run.CreatedAt,
			Data:      map[string]any{"plan": detail.Run.Plan},
		})
	}
	for _, call := range detail.ToolCalls {
		events = append(events, Event{
			RunID:     detail.Run.ID,
			Type:      EventToolCompleted,
			Timestamp: call.CreatedAt,
			Data:      call,
		})
	}
	for _, artifact := range detail.Artifacts {
		events = append(events, Event{
			RunID:     detail.Run.ID,
			Type:      EventArtifactCreated,
			Timestamp: artifact.CreatedAt,
			Data:      artifact,
		})
	}
	return events
}
