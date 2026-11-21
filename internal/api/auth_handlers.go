package api

// auth_handlers.go 处理注册、登录、当前用户信息，
// 并提供 JWT Bearer 鉴权中间件和凭据校验。
import (
	"errors"
	"net/http"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/yzletter/go-agentery/internal/account"
	"github.com/yzletter/go-agentery/internal/auth"
)

const (
	// 以下常量约束用户名和密码的输入长度。
	minUsernameLength = 3
	maxUsernameLength = 32
	minPasswordLength = 8
	maxPasswordLength = 72
)

// usernamePattern 限制用户名只能包含字母、数字、下划线和中划线。
var usernamePattern = regexp.MustCompile(`^[\p{L}\p{N}_-]+$`)

// credentialsRequest 是注册和登录接口共用的请求体。
type credentialsRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// authResponse 是登录或注册成功后返回的令牌和用户信息。
type authResponse struct {
	Token     string       `json:"token"`
	TokenType string       `json:"token_type"`
	ExpiresIn int64        `json:"expires_in"`
	User      userResponse `json:"user"`
}

// userResponse 是接口层公开的用户信息，不包含密码哈希。
type userResponse struct {
	ID       uint64 `json:"id"`
	Username string `json:"username"`
}

// register 校验凭据、创建用户，并返回登录令牌。
func (s *apiServer) register(c *gin.Context) {
	var request credentialsRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求体必须是有效的 JSON"})
		return
	}

	username, password, err := validateCredentials(request)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	user, err := s.accounts.Register(c.Request.Context(), username, password)
	if errors.Is(err, account.ErrUsernameTaken) {
		c.JSON(http.StatusConflict, gin.H{"error": "此用户名已被占用"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "注册失败，请稍后重试"})
		return
	}

	s.respondWithToken(c, http.StatusCreated, user)
}

// login 校验用户名和密码，成功后返回登录令牌。
func (s *apiServer) login(c *gin.Context) {
	var request credentialsRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求体必须是有效的 JSON"})
		return
	}

	username, password, err := validateCredentials(request)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	user, err := s.accounts.Authenticate(c.Request.Context(), username, password)
	if errors.Is(err, account.ErrInvalidCredential) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "用户名或密码错误"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "登录失败，请稍后重试"})
		return
	}

	s.respondWithToken(c, http.StatusOK, user)
}

// me 返回当前登录用户的信息。
func (s *apiServer) me(c *gin.Context) {
	userID, ok := requestUserID(c)
	if !ok {
		return
	}

	user, err := s.accounts.GetByID(c.Request.Context(), userID)
	if errors.Is(err, account.ErrUserNotFound) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "用户不存在"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "读取用户信息失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"user": userResponse{
			ID:       user.ID,
			Username: user.Username,
		},
	})
}

// authMiddleware 校验 Bearer JWT，并把用户 ID 和用户名写入 Gin Context。
func (s *apiServer) authMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		token, err := bearerToken(c.GetHeader("Authorization"))
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "请先登录"})
			return
		}

		principal, err := auth.ParseToken(s.jwtSecret, token)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "登录状态已失效"})
			return
		}

		c.Set(userIDContextKey, principal.UserID)
		c.Set(usernameContextKey, principal.Username)
		c.Next()
	}
}

// respondWithToken 为用户签发 JWT，并按指定状态码返回认证响应。
func (s *apiServer) respondWithToken(c *gin.Context, status int, user account.User) {
	token, err := auth.GenerateToken(s.jwtSecret, user.ID, user.Username)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "生成登录凭证失败"})
		return
	}

	c.JSON(status, authResponse{
		Token:     token,
		TokenType: "Bearer",
		ExpiresIn: int64(auth.TokenTTL.Seconds()),
		User: userResponse{
			ID:       user.ID,
			Username: user.Username,
		},
	})
}

// validateCredentials 清理用户名并校验用户名、密码的长度和字符规范。
func validateCredentials(request credentialsRequest) (string, string, error) {
	username := strings.TrimSpace(request.Username)
	password := request.Password

	if count := utf8.RuneCountInString(username); count < minUsernameLength || count > maxUsernameLength {
		return "", "", errors.New("用户名长度必须在 3 到 32 个字符之间")
	}
	if !usernamePattern.MatchString(username) {
		return "", "", errors.New("用户名只能包含字母、数字、下划线和中划线")
	}
	if len(password) < minPasswordLength || len(password) > maxPasswordLength {
		return "", "", errors.New("密码长度必须在 8 到 72 字节之间")
	}
	return username, password, nil
}

// bearerToken 从 Authorization 请求头中提取 Bearer Token。
func bearerToken(header string) (string, error) {
	header = strings.TrimSpace(header)
	if len(header) <= 7 || !strings.EqualFold(header[:7], "Bearer ") {
		return "", errors.New("missing bearer token")
	}
	token := strings.TrimSpace(header[7:])
	if token == "" {
		return "", errors.New("missing bearer token")
	}
	return token, nil
}
