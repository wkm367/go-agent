package config

// config.go 从环境变量加载 HTTP、MySQL、Redis、历史上下文和 JWT 配置，
// 统一校验配置范围并生成数据库 DSN。
import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
)

const (
	defaultHTTPAddr           = "127.0.0.1:5678"
	defaultHistoryContextSize = 40
	maxHistoryContextSize     = 200

	// DefaultJWTSecret 是未配置 JWT_SECRET 时使用的开发默认值。
	// 生产环境必须通过环境变量覆盖。
	DefaultJWTSecret = "go-agent-change-me-in-production"
)

// Config 汇总后端进程启动所需的全部运行参数。
type Config struct {
	// HTTPAddr 是 HTTP 服务监听地址。
	HTTPAddr string
	// MySQLDSN 是主业务数据库连接串。
	MySQLDSN string
	// AnalyticsMySQLDSN 是分析数据库连接串，通常使用只读账号。
	AnalyticsMySQLDSN string
	// RedisAddr 是 Redis 服务地址。
	RedisAddr string
	// RedisPassword 是 Redis 密码。
	RedisPassword string
	// RedisDB 是 Redis 逻辑数据库编号。
	RedisDB int
	// HistoryContextLimit 是每次对话注入模型的历史消息上限。
	HistoryContextLimit int
	// JWTSecret 是签发和校验登录令牌的 HMAC 密钥。
	JWTSecret string
}

// Load 从环境变量读取配置，校验必要参数并生成 MySQL DSN。
func Load() (Config, error) {
	redisDB, err := intEnv("REDIS_DB", 0)
	if err != nil {
		return Config{}, err
	}

	historyLimit, err := intEnv("HISTORY_CONTEXT_LIMIT", defaultHistoryContextSize)
	if err != nil {
		return Config{}, err
	}
	if historyLimit < 1 || historyLimit > maxHistoryContextSize {
		return Config{}, fmt.Errorf(
			"HISTORY_CONTEXT_LIMIT must be between 1 and %d",
			maxHistoryContextSize,
		)
	}

	mysqlConfig := mysql.Config{
		User:                 envOr("MYSQL_USER", "go_agentery"),
		Passwd:               envOr("MYSQL_PASSWORD", "go_agentery"),
		Net:                  "tcp",
		Addr:                 fmt.Sprintf("%s:%s", envOr("MYSQL_HOST", "127.0.0.1"), envOr("MYSQL_PORT", "3306")),
		DBName:               envOr("MYSQL_DATABASE", "go_agentery"),
		ParseTime:            true,
		Loc:                  time.Local,
		Timeout:              5 * time.Second,
		ReadTimeout:          10 * time.Second,
		WriteTimeout:         10 * time.Second,
		AllowNativePasswords: true,
		Params: map[string]string{
			"charset": "utf8mb4",
		},
	}

	jwtSecret := envOr("JWT_SECRET", DefaultJWTSecret)
	if len(jwtSecret) < 16 {
		return Config{}, fmt.Errorf("JWT_SECRET must be at least 16 characters")
	}

	return Config{
		HTTPAddr:            envOr("HTTP_ADDR", defaultHTTPAddr),
		MySQLDSN:            mysqlConfig.FormatDSN(),
		AnalyticsMySQLDSN:   envOr("ANALYTICS_MYSQL_DSN", mysqlConfig.FormatDSN()),
		RedisAddr:           envOr("REDIS_ADDR", "127.0.0.1:6379"),
		RedisPassword:       envOr("REDIS_PASSWORD", "go_agentery_redis"),
		RedisDB:             redisDB,
		HistoryContextLimit: historyLimit,
		JWTSecret:           jwtSecret,
	}, nil
}

// envOr 返回环境变量去除首尾空白后的值；环境变量为空时返回 fallback。
func envOr(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

// intEnv 读取整数环境变量；未配置时返回 fallback。
func intEnv(name string, fallback int) (int, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback, nil
	}

	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer: %w", name, err)
	}
	return parsed, nil
}
