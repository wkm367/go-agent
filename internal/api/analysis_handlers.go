package api

// analysis_handlers.go 处理 Agent Run 的启动、列表、详情、
// SSE 事件订阅和取消请求，并完成输入校验与错误映射。
import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/yzletter/go-agentery/internal/analysis"
	"github.com/yzletter/go-agentery/internal/history"
)

// maxAnalysisQuestionBytes 是单个分析问题的最大字节数。
const maxAnalysisQuestionBytes = 32 * 1024

// analysisRunRequest 是启动分析任务的请求体。
type analysisRunRequest struct {
	SessionID string `json:"session_id"`
	DatasetID string `json:"dataset_id"`
	Question  string `json:"question"`
}

// startAnalysisRun 校验分析请求、保存用户问题，并异步启动 Agent 任务。
func (s *apiServer) startAnalysisRun(c *gin.Context) {
	userID, ok := requestUserID(c)
	if !ok {
		return
	}
	if s.analysis == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "分析 Agent 暂不可用"})
		return
	}

	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1<<20)
	var request analysisRunRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求体必须是有效的 JSON"})
		return
	}
	request.SessionID = strings.TrimSpace(request.SessionID)
	if _, err := uuid.Parse(request.SessionID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "session_id 必须是有效的 UUID"})
		return
	}
	request.DatasetID = strings.TrimSpace(request.DatasetID)
	if request.DatasetID != "" {
		if _, err := uuid.Parse(request.DatasetID); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "dataset_id 必须是有效的 UUID"})
			return
		}
	}
	request.Question = strings.TrimSpace(request.Question)
	if request.Question == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "分析问题不能为空"})
		return
	}
	if len(request.Question) > maxAnalysisQuestionBytes {
		c.JSON(http.StatusBadRequest, gin.H{"error": "分析问题不能超过 32KB"})
		return
	}
	active, err := s.analysis.HasActiveRun(c.Request.Context(), userID, request.SessionID)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "检查分析任务状态失败"})
		return
	}
	if active {
		c.JSON(http.StatusConflict, gin.H{"error": "该会话已有分析任务正在执行"})
		return
	}

	historyMessages, err := s.store.LoadConversation(
		c.Request.Context(),
		userID,
		request.SessionID,
		request.Question,
	)
	if errors.Is(err, history.ErrNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "会话不存在"})
		return
	}
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "保存分析问题失败"})
		return
	}

	run, err := s.analysis.StartRun(
		c.Request.Context(),
		userID,
		request.SessionID,
		request.DatasetID,
		request.Question,
		historyMessages,
	)
	if errors.Is(err, analysis.ErrBusy) {
		c.JSON(http.StatusConflict, gin.H{"error": "该会话已有分析任务正在执行"})
		return
	}
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "启动分析任务失败"})
		return
	}
	c.JSON(http.StatusAccepted, gin.H{
		"run_id": run.ID,
		"status": run.Status,
	})
}

// listAnalysisRuns 返回当前用户的分析任务列表，可按会话 ID 过滤。
func (s *apiServer) listAnalysisRuns(c *gin.Context) {
	userID, ok := requestUserID(c)
	if !ok {
		return
	}
	if s.analysis == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "分析 Agent 暂不可用"})
		return
	}

	limit := 20
	if value := strings.TrimSpace(c.Query("limit")); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 1 || parsed > 100 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "limit 必须在 1 到 100 之间"})
			return
		}
		limit = parsed
	}
	sessionID := strings.TrimSpace(c.Query("session_id"))
	if sessionID != "" {
		if _, err := uuid.Parse(sessionID); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "session_id 必须是有效的 UUID"})
			return
		}
	}

	runs, err := s.analysis.ListRuns(c.Request.Context(), userID, sessionID, limit)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "读取分析任务失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"runs": runs})
}

// getAnalysisRun 返回指定分析任务及其步骤、工具调用和产物。
func (s *apiServer) getAnalysisRun(c *gin.Context) {
	userID, ok := requestUserID(c)
	if !ok {
		return
	}
	if s.analysis == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "分析 Agent 暂不可用"})
		return
	}
	runID, ok := requestRunID(c)
	if !ok {
		return
	}

	detail, err := s.analysis.GetRun(c.Request.Context(), userID, runID)
	if errors.Is(err, analysis.ErrNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "分析任务不存在"})
		return
	}
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "读取分析任务失败"})
		return
	}
	c.JSON(http.StatusOK, detail)
}

// streamAnalysisRun 通过 SSE 先回放历史事件，再持续推送实时事件，
// 收到终止事件或客户端断开后结束响应。
func (s *apiServer) streamAnalysisRun(c *gin.Context) {
	userID, ok := requestUserID(c)
	if !ok {
		return
	}
	if s.analysis == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "分析 Agent 暂不可用"})
		return
	}
	runID, ok := requestRunID(c)
	if !ok {
		return
	}

	events, historyEvents, err := s.analysis.Subscribe(
		c.Request.Context(),
		userID,
		runID,
	)
	if errors.Is(err, analysis.ErrNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "分析任务不存在"})
		return
	}
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "订阅分析事件失败"})
		return
	}

	c.Header("Content-Type", "text/event-stream; charset=utf-8")
	c.Header("Cache-Control", "no-cache, no-transform")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	for _, event := range historyEvents {
		writeSSE(c.Writer, event.Type, event)
		c.Writer.Flush()
		if isTerminalAnalysisEvent(event.Type) {
			return
		}
	}

	for {
		select {
		case <-c.Request.Context().Done():
			return
		case event, open := <-events:
			if !open {
				return
			}
			writeSSE(c.Writer, event.Type, event)
			c.Writer.Flush()
			if isTerminalAnalysisEvent(event.Type) {
				return
			}
		}
	}
}

// cancelAnalysisRun 请求取消指定用户拥有的分析任务。
func (s *apiServer) cancelAnalysisRun(c *gin.Context) {
	userID, ok := requestUserID(c)
	if !ok {
		return
	}
	if s.analysis == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "分析 Agent 暂不可用"})
		return
	}
	runID, ok := requestRunID(c)
	if !ok {
		return
	}
	if err := s.analysis.Cancel(c.Request.Context(), userID, runID); err != nil {
		if errors.Is(err, analysis.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "分析任务不存在"})
			return
		}
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "取消分析任务失败"})
		return
	}
	c.Status(http.StatusNoContent)
}

// requestRunID 校验并返回路径参数中的分析任务 UUID。
func requestRunID(c *gin.Context) (string, bool) {
	runID := strings.TrimSpace(c.Param("id"))
	if _, err := uuid.Parse(runID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "run_id 必须是有效的 UUID"})
		return "", false
	}
	return runID, true
}

// isTerminalAnalysisEvent 判断事件是否表示任务已经结束。
func isTerminalAnalysisEvent(eventType string) bool {
	switch eventType {
	case analysis.EventRunCompleted, analysis.EventRunFailed, analysis.EventRunCancelled:
		return true
	default:
		return false
	}
}
