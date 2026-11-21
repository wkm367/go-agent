package config

// config_test.go 验证历史上下文窗口的默认加载、有效配置覆盖
// 和越界值拒绝。
import "testing"

func TestLoadHistoryContextLimit(t *testing.T) {
	t.Setenv("HISTORY_CONTEXT_LIMIT", "72")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.HistoryContextLimit != 72 {
		t.Fatalf("expected 72, got %d", cfg.HistoryContextLimit)
	}
}

func TestLoadRejectsInvalidHistoryContextLimit(t *testing.T) {
	t.Setenv("HISTORY_CONTEXT_LIMIT", "0")

	if _, err := Load(); err == nil {
		t.Fatal("expected invalid history context limit error")
	}
}
