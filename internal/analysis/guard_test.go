package analysis

// guard_test.go 覆盖只读 SQL 白名单、别名解析、危险语句拦截，
// 以及语义指标目录的别名搜索。
import (
	"strings"
	"testing"
)

func TestValidateReadonlySQLAcceptsBusinessSelect(t *testing.T) {
	guarded, err := ValidateReadonlySQL(`
		SELECT DATE(order_date) AS day, SUM(paid_amount) AS paid_gmv
		FROM orders
		WHERE status IN ('paid', 'refunded')
		GROUP BY DATE(order_date)
		ORDER BY day
	`)
	if err != nil {
		t.Fatalf("expected SQL to pass guard: %v", err)
	}
	if !strings.Contains(guarded, "LIMIT 501") {
		t.Fatalf("expected row guard, got %q", guarded)
	}
}

func TestValidateReadonlySQLAcceptsTableAliases(t *testing.T) {
	query := `
		SELECT o.id, c.channel_name
		FROM orders o
		JOIN channels c ON c.id = o.channel_id
		WHERE o.status <> 'cancelled'
	`
	_, err := ValidateReadonlySQL(query)
	if err != nil {
		t.Fatalf("expected aliases to pass guard: %v", err)
	}
}

func TestValidateReadonlySQLAcceptsCancellationRateQuery(t *testing.T) {
	guarded, err := ValidateReadonlySQL(`
		SELECT DATE(order_date) AS day,
		       SUM(CASE WHEN status = 'cancelled' THEN 1 ELSE 0 END)
		         / NULLIF(COUNT(id), 0) AS cancel_rate
		FROM orders
		GROUP BY DATE(order_date)
	`)
	if err != nil {
		t.Fatalf("expected cancellation rate SQL to pass guard: %v", err)
	}
	if !strings.Contains(guarded, "LIMIT 501") {
		t.Fatalf("expected row guard, got %q", guarded)
	}
}

func TestValidateReadonlySQLRejectsUnsafeStatements(t *testing.T) {
	tests := []struct {
		name string
		sql  string
	}{
		{name: "update", sql: "UPDATE orders SET status = 'paid'"},
		{name: "multiple statements", sql: "SELECT * FROM orders; DROP TABLE users"},
		{name: "system table", sql: "SELECT password_hash FROM users"},
		{name: "qualified table", sql: "SELECT * FROM mysql.user"},
		{name: "comment", sql: "SELECT * FROM orders -- bypass"},
		{name: "outfile", sql: "SELECT * FROM orders INTO OUTFILE '/tmp/a'"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := ValidateReadonlySQL(test.sql); err == nil {
				t.Fatalf("expected SQL to be rejected: %s", test.sql)
			}
		})
	}
}

func TestCatalogSearchFindsMetricAliases(t *testing.T) {
	catalog := DefaultCatalog()
	metrics := catalog.Search("看看最近退款率为什么升高", 3)
	if len(metrics) == 0 {
		t.Fatal("expected at least one metric")
	}
	if metrics[0].Name != "refund_rate" {
		t.Fatalf("expected refund_rate first, got %s", metrics[0].Name)
	}
}

func TestCatalogSearchFindsCancellationRate(t *testing.T) {
	catalog := DefaultCatalog()
	metrics := catalog.Search("最近订单取消率为什么升高", 3)
	if len(metrics) == 0 {
		t.Fatal("expected at least one metric")
	}
	if metrics[0].Name != "cancel_rate" {
		t.Fatalf("expected cancel_rate first, got %s", metrics[0].Name)
	}
}
