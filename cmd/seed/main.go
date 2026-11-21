package main

// seed 是演示数据初始化命令，按月份和订单量参数重建电商业务表，
// 并写入可重复生成的样例经营数据。
import (
	"context"
	"flag"
	"log"
	"time"

	"github.com/yzletter/go-agentery/internal/analysis"
	"github.com/yzletter/go-agentery/internal/config"
)

// main 解析演示数据参数，并根据环境配置重建分析库样例数据。
func main() {
	months := flag.Int("months", 6, "number of months to generate")
	orders := flag.Int("orders", 12000, "number of demo orders")
	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := analysis.SeedDemoData(ctx, cfg.AnalyticsMySQLDSN, analysis.SeedOptions{
		Months: *months,
		Orders: *orders,
	}); err != nil {
		log.Fatalf("seed analytics data: %v", err)
	}
	log.Printf(
		"seeded %d orders across %d months into analytics database",
		*orders,
		*months,
	)
}
