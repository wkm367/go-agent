package model

// model_test.go 验证默认模型名和 ARK_MODEL 环境变量覆盖规则。
import "testing"

func TestChatModelName(t *testing.T) {
	t.Setenv("ARK_MODEL", "")
	if got := chatModelName(); got != defaultChatModel {
		t.Fatalf("expected default model %q, got %q", defaultChatModel, got)
	}

	t.Setenv("ARK_MODEL", " ep-custom-model ")
	if got := chatModelName(); got != "ep-custom-model" {
		t.Fatalf("expected configured model, got %q", got)
	}
}
