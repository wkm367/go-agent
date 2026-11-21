package api

// server.go 组装 Gin 路由、中间件和会话健康检查，
// 并集中挂载认证、会话、分析、数据集和 SSE 接口。
import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/cloudwego/eino/schema"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/yzletter/go-agentery/internal/account"
	"github.com/yzletter/go-agentery/internal/analysis"
	"github.com/yzletter/go-agentery/internal/history"
)

const (
	// userIDContextKey 是 Gin Context 中当前用户 ID 的键。
	userIDContextKey = "user_id"
	// usernameContextKey 是 Gin Context 中当前用户名的键。
	usernameContextKey = "username"
	// defaultSessionLimit 是会话列表接口的默认返回数量。
	defaultSessionLimit = 50
	// maxSessionLimit 是会话列表接口允许的最大返回数量。
	maxSessionLimit = 100
)

// conversationStore 是 HTTP 层依赖的会话历史能力集合。
type conversationStore interface {
	PingMySQL(ctx context.Context) error
	PingRedis(ctx context.Context) error
	ListSessions(ctx context.Context, userID uint64, limit int) ([]history.SessionSummary, error)
	GetMessages(ctx context.Context, userID uint64, sessionID string) ([]history.MessageView, error)
	DeleteSession(ctx context.Context, userID uint64, sessionID string) error
	LoadConversation(
		ctx context.Context,
		userID uint64,
		sessionID, userMessage string,
	) ([]*schema.Message, error)
}

// analysisService 是 HTTP 层依赖的分析任务能力集合。
type analysisService interface {
	StartRun(
		ctx context.Context,
		userID uint64,
		sessionID string,
		datasetID string,
		question string,
		history []*schema.Message,
	) (*analysis.Run, error)
	GetRun(ctx context.Context, userID uint64, runID string) (*analysis.RunDetail, error)
	ListRuns(ctx context.Context, userID uint64, sessionID string, limit int) ([]analysis.Run, error)
	HasActiveRun(ctx context.Context, userID uint64, sessionID string) (bool, error)
	Subscribe(ctx context.Context, userID uint64, runID string) (<-chan analysis.Event, []analysis.Event, error)
	Cancel(ctx context.Context, userID uint64, runID string) error
}

// datasetService 是 HTTP 层依赖的数据集管理能力集合。
type datasetService interface {
	Create(
		ctx context.Context,
		userID uint64,
		name string,
		filename string,
		reader io.Reader,
	) (*analysis.Dataset, error)
	CreateExcel(
		ctx context.Context,
		userID uint64,
		name string,
		filename string,
		sheet string,
		reader io.Reader,
	) (*analysis.Dataset, error)
	List(ctx context.Context, userID uint64) ([]analysis.Dataset, error)
	Rename(
		ctx context.Context,
		userID uint64,
		datasetID string,
		name string,
	) (*analysis.Dataset, error)
	Delete(ctx context.Context, userID uint64, datasetID string) error
}

// apiServer 持有所有 HTTP Handler 共享的依赖。
type apiServer struct {
	store    conversationStore
	accounts *account.Service
	// jwtSecret 用于签发和校验登录令牌。
	jwtSecret string
	// analysis 和 datasets 是可空的可选服务。
	analysis analysisService
	datasets datasetService
}

// createSession 创建一个新的会话 UUID，会话正文由后续消息接口写入。
func (s *apiServer) createSession(c *gin.Context) {
	if _, ok := requestUserID(c); !ok {
		return
	}
	c.JSON(http.StatusCreated, gin.H{"session_id": uuid.NewString()})
}

// listSessions 返回当前用户最近的会话摘要。
func (s *apiServer) listSessions(c *gin.Context) {
	userID, ok := requestUserID(c)
	if !ok {
		return
	}

	limit := defaultSessionLimit
	if value := strings.TrimSpace(c.Query("limit")); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 1 || parsed > maxSessionLimit {
			c.JSON(http.StatusBadRequest, gin.H{"error": "limit 必须在 1 到 100 之间"})
			return
		}
		limit = parsed
	}

	sessions, err := s.store.ListSessions(c.Request.Context(), userID, limit)
	if err != nil {
		log.Printf("list sessions: %v", err)
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "读取历史会话失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"sessions": sessions})
}

// getMessages 返回当前用户指定会话的全部消息。
func (s *apiServer) getMessages(c *gin.Context) {
	userID, ok := requestUserID(c)
	if !ok {
		return
	}
	sessionID, ok := requestSessionID(c)
	if !ok {
		return
	}

	messages, err := s.store.GetMessages(c.Request.Context(), userID, sessionID)
	if errors.Is(err, history.ErrNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "会话不存在"})
		return
	}
	if err != nil {
		log.Printf("get session messages: %v", err)
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "读取历史消息失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"messages": messages})
}

// deleteSession 删除当前用户拥有的一条会话及其消息。
func (s *apiServer) deleteSession(c *gin.Context) {
	userID, ok := requestUserID(c)
	if !ok {
		return
	}
	sessionID, ok := requestSessionID(c)
	if !ok {
		return
	}

	err := s.store.DeleteSession(c.Request.Context(), userID, sessionID)
	if errors.Is(err, history.ErrNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "会话不存在"})
		return
	}
	if err != nil {
		log.Printf("delete session: %v", err)
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "删除会话失败"})
		return
	}
	c.Status(http.StatusNoContent)
}

// healthCheck 返回 MySQL 和 Redis 的健康状态。
// MySQL 不可用时返回 503，Redis 不可用时返回 degraded。
func (s *apiServer) healthCheck(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
	defer cancel()

	mysqlStatus := "ok"
	redisStatus := "ok"
	statusCode := http.StatusOK

	if err := s.store.PingMySQL(ctx); err != nil {
		mysqlStatus = "unavailable"
		statusCode = http.StatusServiceUnavailable
	}
	if err := s.store.PingRedis(ctx); err != nil {
		redisStatus = "unavailable"
	}

	status := "ok"
	if statusCode != http.StatusOK || redisStatus != "ok" {
		status = "degraded"
	}
	c.JSON(statusCode, gin.H{
		"status": status,
		"mysql":  mysqlStatus,
		"redis":  redisStatus,
	})
}

// requestUserID 从 Gin Context 读取已认证用户 ID；
// 缺失或无效时直接写入 401 响应并返回 false。
func requestUserID(c *gin.Context) (uint64, bool) {
	value, exists := c.Get(userIDContextKey)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "请先登录"})
		return 0, false
	}
	userID, ok := value.(uint64)
	if !ok || userID == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "登录状态无效"})
		return 0, false
	}
	return userID, true
}

// requestSessionID 校验并返回路径参数中的会话 UUID。
func requestSessionID(c *gin.Context) (string, bool) {
	sessionID := strings.TrimSpace(c.Param("id"))
	if _, err := uuid.Parse(sessionID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "session_id 必须是有效的 UUID"})
		return "", false
	}
	return sessionID, true
}

// writeSSE 将一个事件按 Server-Sent Events 格式写入响应。
func writeSSE(w io.Writer, event string, payload any) {
	data, err := json.Marshal(payload)
	if err != nil {
		log.Printf("marshal SSE payload: %v", err)
		return
	}
	_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data)
}

// corsMiddleware 根据 CORS_ALLOWED_ORIGINS 配置处理跨域请求和预检。
func corsMiddleware() gin.HandlerFunc {
	allowedOrigins := splitEnv("CORS_ALLOWED_ORIGINS", []string{
		"http://127.0.0.1:5173",
		"http://localhost:5173",
	})

	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if origin != "" && isOriginAllowed(origin, allowedOrigins) {
			c.Header("Access-Control-Allow-Origin", origin)
			c.Header("Access-Control-Allow-Credentials", "true")
			c.Header(
				"Access-Control-Allow-Headers",
				"Content-Type, Authorization",
			)
			c.Header("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
			c.Header("Vary", "Origin")
		}

		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}

// NewRouter 创建 Gin 路由并注册全局中间件、公开接口和受保护接口。
// services 可传入分析服务和数据集服务，具体类型通过接口断言注入。
func NewRouter(
	store conversationStore,
	accounts *account.Service,
	jwtSecret string,
	services ...any,
) *gin.Engine {
	router := gin.New()
	router.Use(gin.Logger(), gin.Recovery(), corsMiddleware())

	server := &apiServer{
		store:     store,
		accounts:  accounts,
		jwtSecret: jwtSecret,
	}
	for _, service := range services {
		switch typed := service.(type) {
		case analysisService:
			server.analysis = typed
		case datasetService:
			server.datasets = typed
		}
	}
	router.GET("/healthz", server.healthCheck)

	apiGroup := router.Group("/api")
	authGroup := apiGroup.Group("/auth")
	authGroup.POST("/register", server.register)
	authGroup.POST("/login", server.login)
	authGroup.GET("/me", server.authMiddleware(), server.me)

	protected := apiGroup.Group("")
	protected.Use(server.authMiddleware())
	protected.POST("/sessions", server.createSession)
	protected.GET("/sessions", server.listSessions)
	protected.GET("/sessions/:id/messages", server.getMessages)
	protected.DELETE("/sessions/:id", server.deleteSession)
	protected.POST("/analysis/runs", server.startAnalysisRun)
	protected.GET("/analysis/runs", server.listAnalysisRuns)
	protected.GET("/analysis/runs/:id", server.getAnalysisRun)
	protected.GET("/analysis/runs/:id/events", server.streamAnalysisRun)
	protected.POST("/analysis/runs/:id/cancel", server.cancelAnalysisRun)
	protected.POST("/datasets", server.uploadDataset)
	protected.POST("/datasets/excel", server.uploadExcelDataset)
	protected.GET("/datasets", server.listDatasets)
	protected.PATCH("/datasets/:id", server.renameDataset)
	protected.DELETE("/datasets/:id", server.deleteDataset)

	return router
}

// splitEnv 按逗号拆分环境变量，并过滤空白项。
func splitEnv(name string, fallback []string) []string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}

	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if origin := strings.TrimSpace(part); origin != "" {
			result = append(result, origin)
		}
	}
	return result
}

// isOriginAllowed 判断请求源是否命中白名单或通配符。
func isOriginAllowed(origin string, allowedOrigins []string) bool {
	for _, allowed := range allowedOrigins {
		if allowed == "*" || allowed == origin {
			return true
		}
	}
	return false
}
