package model

// model.go 创建并复用火山方舟 Eino ChatModel，
// 负责解析 ARK_KEY、ARK_MODEL 等模型配置。
import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/cloudwego/eino-ext/components/model/ark"
)

// defaultChatModel 是未配置 ARK_MODEL 时使用的方舟模型。
const defaultChatModel = "doubao-seed-1-8-251228"

// 模型客户端采用进程级单例，避免每次分析请求重复创建连接。
var (
	arkModel *ark.ChatModel
	arkErr   error
	arkOnce  sync.Once
)

// CreateArkModel 返回进程级共享的火山方舟聊天模型。
// 首次调用创建客户端，后续调用直接复用创建结果。
func CreateArkModel() (*ark.ChatModel, error) {
	arkOnce.Do(func() {
		ctx := context.Background()

		// 火山参数需要传指针
		//timeout := 30 * time.Second
		//maxTokens := 3000
		//temperature := float32(1.0)

		arkModel, arkErr = ark.NewChatModel(ctx, &ark.ChatModelConfig{
			APIKey: os.Getenv("ARK_KEY"),
			Model:  chatModelName(),
			//Timeout:     &timeout,
			//MaxTokens:   &maxTokens,   // 返回最大的 Token 数
			//Temperature: &temperature, // 温度，温度越高越发散 [0, 2]
		})
		if arkErr != nil {
			arkErr = fmt.Errorf("create ark chat model: %w", arkErr)
		}
	})

	return arkModel, arkErr
}

// chatModelName 返回配置的模型名称；未配置时使用默认模型。
func chatModelName() string {
	if modelName := strings.TrimSpace(os.Getenv("ARK_MODEL")); modelName != "" {
		return modelName
	}
	return defaultChatModel
}

// ChatModelName 返回当前后端使用的聊天模型名称。
func ChatModelName() string {
	return chatModelName()
}
