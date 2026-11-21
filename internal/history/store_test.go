package history

// store_test.go 覆盖会话标题生成、本地锁降级，
// 以及通过 MYSQL_TEST_DSN 执行的真实存储集成测试。
import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestBuildSessionTitle(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{name: "empty", content: "   ", want: "新会话"},
		{name: "whitespace", content: "  今天   天气  ", want: "今天 天气"},
		{
			name:    "truncate",
			content: strings.Repeat("测", 45),
			want:    strings.Repeat("测", 40),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := buildSessionTitle(test.content); got != test.want {
				t.Fatalf("expected %q, got %q", test.want, got)
			}
		})
	}
}

func TestAcquireChatFallsBackToLocalLock(t *testing.T) {
	store := NewStore(nil, nil, 40)
	sessionID := uuid.NewString()
	release, err := store.AcquireChat(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("first lock failed: %v", err)
	}
	defer release()

	if _, err := store.AcquireChat(context.Background(), sessionID); !errors.Is(err, ErrBusy) {
		t.Fatalf("expected ErrBusy, got %v", err)
	}
}

func TestStoreWithMySQL(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("MYSQL_TEST_DSN"))
	if dsn == "" {
		t.Skip("MYSQL_TEST_DSN is not set")
	}

	ctx := context.Background()
	db, err := OpenMySQL(ctx, dsn)
	if err != nil {
		t.Fatalf("open mysql: %v", err)
	}

	store := NewStore(db, nil, 40)
	t.Cleanup(func() {
		_ = store.Close()
	})
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	userID := uint64(42)
	sessionID := uuid.NewString()
	t.Cleanup(func() {
		_ = store.DeleteSession(context.Background(), userID, sessionID)
	})

	conversation, err := store.LoadConversation(ctx, userID, sessionID, "第一条消息")
	if err != nil {
		t.Fatalf("load conversation: %v", err)
	}
	if len(conversation) != 1 || conversation[0].Content != "第一条消息" {
		t.Fatalf("unexpected conversation: %#v", conversation)
	}

	if err := store.SaveAssistantMessage(
		ctx,
		sessionID,
		"完整回答",
		StatusComplete,
	); err != nil {
		t.Fatalf("save assistant message: %v", err)
	}

	messages, err := store.GetMessages(ctx, userID, sessionID)
	if err != nil {
		t.Fatalf("get messages: %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(messages))
	}

	sessions, err := store.ListSessions(ctx, userID, 50)
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if len(sessions) != 1 || sessions[0].Title != "第一条消息" {
		t.Fatalf("unexpected sessions: %#v", sessions)
	}

	if _, err := store.GetMessages(ctx, userID+1, sessionID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ownership error, got %v", err)
	}

	if err := store.DeleteSession(ctx, userID, sessionID); err != nil {
		t.Fatalf("delete session: %v", err)
	}
	if _, err := store.GetMessages(ctx, userID, sessionID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected deleted session to be missing, got %v", err)
	}
}
