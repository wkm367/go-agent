package main

// server 是后端 HTTP 服务入口，负责加载配置、初始化数据库与 Agent 依赖、
// 注册路由、启动服务，并在进程退出时执行优雅停机。
import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yzletter/go-agentery/internal/account"
	"github.com/yzletter/go-agentery/internal/analysis"
	"github.com/yzletter/go-agentery/internal/api"
	"github.com/yzletter/go-agentery/internal/config"
	"github.com/yzletter/go-agentery/internal/history"
)

// main 加载运行配置和所有依赖，启动 Gin HTTP 服务，并等待退出信号完成优雅停机。
func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	// startupContext 限制数据库迁移和连接初始化的总启动时间。
	startupContext, cancelStartup := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancelStartup()

	db, err := history.OpenMySQL(startupContext, cfg.MySQLDSN)
	if err != nil {
		log.Fatalf("connect mysql: %v", err)
	}

	redisCache := history.NewRedisCache(
		cfg.RedisAddr,
		cfg.RedisPassword,
		cfg.RedisDB,
	)
	defer redisCache.Close()

	store := history.NewStore(db, redisCache, cfg.HistoryContextLimit)
	defer store.Close()

	// analyticsService 保持为 nil 时，分析 API 不注册，但主服务仍可启动。
	var analyticsService *analysis.Service
	analysisStore := analysis.NewStore(db)
	if err := analysisStore.Migrate(startupContext); err != nil {
		log.Printf("migrate analysis tables: %v", err)
	}
	datasetService := analysis.NewDatasetService(db)
	if err := datasetService.Migrate(startupContext); err != nil {
		log.Printf("migrate datasets table: %v", err)
	}

	analyticsDB, err := history.OpenMySQL(startupContext, cfg.AnalyticsMySQLDSN)
	if err != nil {
		log.Printf("analytics database unavailable, analysis API disabled: %v", err)
	} else {
		gateway, gatewayErr := analysis.NewToolGateway(
			analyticsDB,
			analysis.DefaultCatalog(),
		)
		if gatewayErr != nil {
			log.Printf("initialize analytics tools: %v", gatewayErr)
		} else {
			analyticsService = analysis.NewService(
				analysisStore,
				gateway,
				store,
				nil,
			)
			analyticsService.SetDatasetService(datasetService)
		}
	}

	accounts := account.NewService(db)
	if err := accounts.Migrate(startupContext); err != nil {
		log.Fatalf("migrate accounts: %v", err)
	}
	if err := store.Migrate(startupContext); err != nil {
		log.Fatalf("migrate mysql: %v", err)
	}
	if err := store.PingRedis(startupContext); err != nil {
		log.Printf("redis unavailable, continuing with mysql only: %v", err)
	}
	if cfg.JWTSecret == config.DefaultJWTSecret {
		log.Printf("warning: using the default JWT_SECRET; set a private value in production")
	}

	// router 是最终挂载到 HTTP Server 的 Gin Engine。
	var router *gin.Engine
	if analyticsService != nil {
		router = api.NewRouter(
			store,
			accounts,
			cfg.JWTSecret,
			analyticsService,
			datasetService,
		)
	} else {
		router = api.NewRouter(store, accounts, cfg.JWTSecret, datasetService)
	}

	// 使用标准 http.Server 承载 Gin，以便监听信号并执行 Shutdown。
	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
	}

	shutdownContext, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()

	// 收到 SIGINT 或 SIGTERM 后，最多等待 10 秒完成在途请求。
	go func() {
		<-shutdownContext.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			log.Printf("shutdown server: %v", err)
		}
	}()

	log.Printf("API server listening on http://%s", cfg.HTTPAddr)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("serve API: %v", err)
	}
}
