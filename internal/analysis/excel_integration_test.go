package analysis

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"

	"github.com/yzletter/go-agentery/internal/history"
)

func TestExcelDatasetWithMySQL(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("MYSQL_TEST_DSN"))
	if dsn == "" {
		t.Skip("MYSQL_TEST_DSN is not set")
	}

	ctx := context.Background()
	db, err := history.OpenMySQL(ctx, dsn)
	if err != nil {
		t.Fatalf("open mysql: %v", err)
	}
	service := NewDatasetService(db)
	if err := service.Migrate(ctx); err != nil {
		t.Fatalf("migrate datasets: %v", err)
	}

	data := buildTestWorkbook(t, "空表", "订单", [][]any{
		{"订单号", "金额"},
		{1001, 12.5},
		{1002, 20},
	})
	dataset, err := service.CreateExcel(
		ctx,
		9001,
		"订单数据",
		"orders.xlsx",
		"订单",
		bytes.NewReader(data),
	)
	if err != nil {
		t.Fatalf("create excel dataset: %v", err)
	}
	t.Cleanup(func() {
		if err := service.Delete(context.Background(), 9001, dataset.ID); err != nil {
			t.Logf("cleanup excel dataset: %v", err)
		}
	})

	if dataset.StorageRows != 2 || !isValidUserDatasetTable(dataset.StorageTable) {
		t.Fatalf("unexpected dataset: %#v", dataset)
	}

	var count int
	if err := db.Raw(
		"SELECT COUNT(*) FROM `" + dataset.StorageTable + "`",
	).Scan(&count).Error; err != nil {
		t.Fatalf("count excel rows: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected 2 rows, got %d", count)
	}

	analyticsDB := db
	analyticsDSN := strings.TrimSpace(os.Getenv("ANALYTICS_TEST_DSN"))
	if analyticsDSN != "" {
		analyticsDB, err = history.OpenMySQL(ctx, analyticsDSN)
		if err != nil {
			t.Fatalf("open analytics mysql: %v", err)
		}
		analyticsSQL, err := analyticsDB.DB()
		if err != nil {
			t.Fatalf("get analytics pool: %v", err)
		}
		defer analyticsSQL.Close()
	}

	gateway, err := NewToolGateway(analyticsDB, DefaultCatalog())
	if err != nil {
		t.Fatalf("create gateway: %v", err)
	}
	scopeContext := withDatasetScope(ctx, datasetScopeFromDataset(dataset))
	result, err := gateway.runReadonlySQL(scopeContext, SQLInput{
		SQL:     "SELECT c1, c2 FROM " + dataset.StorageTable + " ORDER BY c1",
		Purpose: "integration test",
	})
	if err != nil {
		t.Fatalf("run excel SQL: %v", err)
	}
	if result.RowCount != 2 {
		t.Fatalf("expected 2 query rows, got %d", result.RowCount)
	}

	if _, err := gateway.runReadonlySQL(scopeContext, SQLInput{
		SQL:     "SELECT * FROM customers",
		Purpose: "cross-table access must fail",
	}); err == nil {
		t.Fatal("expected cross-table query to be rejected")
	}
}
