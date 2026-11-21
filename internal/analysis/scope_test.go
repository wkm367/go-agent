package analysis

import (
	"strings"
	"testing"
)

func TestToolInfosForScope(t *testing.T) {
	gateway := &ToolGateway{}
	tests := []struct {
		name     string
		scope    *DatasetScope
		expected []string
	}{
		{
			name:     "built-in database",
			scope:    nil,
			expected: []string{ToolSearchSemantics, ToolGetSchema, ToolRunReadonlySQL, ToolCreateChart},
		},
		{
			name:     "document dataset",
			scope:    &DatasetScope{Kind: datasetKindDocument},
			expected: []string{ToolSearchSemantics},
		},
		{
			name:     "excel dataset",
			scope:    &DatasetScope{Kind: datasetKindTable, TableName: "user_dataset_0123456789abcdef0123456789abcdef"},
			expected: []string{ToolGetSchema, ToolRunReadonlySQL, ToolCreateChart},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			infos, err := gateway.EinoToolInfosForScope(test.scope)
			if err != nil {
				t.Fatalf("build tool infos: %v", err)
			}
			if len(infos) != len(test.expected) {
				t.Fatalf("expected %d tools, got %d", len(test.expected), len(infos))
			}
			for index, expected := range test.expected {
				if infos[index].Name != expected {
					t.Fatalf("tool %d: expected %s, got %s", index, expected, infos[index].Name)
				}
			}
		})
	}
}

func TestValidateReadonlySQLWithUserTable(t *testing.T) {
	tableName := "user_dataset_0123456789abcdef0123456789abcdef"
	allowed := map[string]struct{}{tableName: {}}

	if _, err := ValidateReadonlySQLWithTables(
		"SELECT c1, SUM(c2) FROM "+tableName+" GROUP BY c1",
		allowed,
	); err != nil {
		t.Fatalf("expected user table SQL to pass: %v", err)
	}

	if _, err := ValidateReadonlySQLWithTables(
		"SELECT * FROM customers",
		allowed,
	); err == nil || !strings.Contains(err.Error(), "白名单") {
		t.Fatalf("expected other table to be rejected, got %v", err)
	}
}

func TestTableDatasetContextText(t *testing.T) {
	scope := &DatasetScope{
		Name:             "订单数据",
		SheetName:        "订单",
		TableName:        "user_dataset_0123456789abcdef0123456789abcdef",
		RowCount:         2,
		Columns:          []DatasetColumn{{Name: "c1", Header: "订单号", Type: "BIGINT"}},
		OriginalFilename: "orders.xlsx",
	}
	text := tableDatasetContextText(scope)
	if !strings.Contains(text, scope.TableName) ||
		!strings.Contains(text, "订单号") ||
		!strings.Contains(text, "只允许查询这张表") {
		t.Fatalf("unexpected table context: %s", text)
	}
}
