package analysis

import (
	"bytes"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"
)

func TestParseExcelWorkbookSelectsFirstNonEmptySheet(t *testing.T) {
	data := buildTestWorkbook(t, "空表", "订单", [][]any{
		{"订单号", "金额", "下单日期", "备注"},
		{1001, 12.5, "2026-01-02", "首单"},
		{1002, 20, "2026-01-03", ""},
	})

	workbook, err := parseExcelWorkbook(data, "")
	if err != nil {
		t.Fatalf("parse workbook: %v", err)
	}
	if workbook.SheetName != "订单" {
		t.Fatalf("expected 订单, got %q", workbook.SheetName)
	}
	if len(workbook.Rows) != 2 {
		t.Fatalf("expected 2 data rows, got %d", len(workbook.Rows))
	}
	expectedTypes := []string{"BIGINT", "DOUBLE", "DATETIME", "TEXT"}
	for index, expected := range expectedTypes {
		if workbook.Columns[index].Type != expected {
			t.Fatalf("column %d: expected %s, got %s", index, expected, workbook.Columns[index].Type)
		}
	}
	if workbook.Columns[0].Name != "c1" || workbook.Columns[0].Header != "订单号" {
		t.Fatalf("unexpected first column: %#v", workbook.Columns[0])
	}
}

func TestNormalizeExcelHeaders(t *testing.T) {
	headers := normalizeExcelHeaders([]string{"", "金额", "金额"}, 3)
	expected := []string{"列 1", "金额", "金额 (2)"}
	for index := range expected {
		if headers[index] != expected[index] {
			t.Fatalf("header %d: expected %q, got %q", index, expected[index], headers[index])
		}
	}
}

func TestInferExcelColumnType(t *testing.T) {
	tests := []struct {
		name     string
		rows     [][]string
		expected string
	}{
		{name: "integer", rows: [][]string{{"1"}, {"2"}}, expected: "BIGINT"},
		{name: "leading zero", rows: [][]string{{"001"}, {"002"}}, expected: "TEXT"},
		{name: "decimal", rows: [][]string{{"1.5"}, {"2"}}, expected: "DOUBLE"},
		{name: "date", rows: [][]string{{"2026-01-02"}, {"2026/01/03"}}, expected: "DATETIME"},
		{name: "mixed", rows: [][]string{{"1"}, {"abc"}}, expected: "TEXT"},
		{name: "empty", rows: [][]string{{""}, {""}}, expected: "TEXT"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := inferExcelColumnType(test.rows, 0); got != test.expected {
				t.Fatalf("expected %s, got %s", test.expected, got)
			}
		})
	}
}

func TestNormalizeExcelRowsRejectsTooManyColumns(t *testing.T) {
	header := make([]string, MaxExcelColumns+1)
	for index := range header {
		header[index] = "h"
	}
	_, _, err := normalizeExcelRows([][]string{header, header})
	if err == nil || !strings.Contains(err.Error(), "列") {
		t.Fatalf("expected column limit error, got %v", err)
	}
}

func TestNormalizeExcelRowsRejectsHeaderOnly(t *testing.T) {
	_, _, err := normalizeExcelRows([][]string{{"订单号", "金额"}})
	if err == nil {
		t.Fatal("expected header-only error")
	}
}

func buildTestWorkbook(
	t *testing.T,
	firstName string,
	secondName string,
	rows [][]any,
) []byte {
	t.Helper()
	file := excelize.NewFile()
	defer file.Close()

	defaultSheet := file.GetSheetName(0)
	if err := file.SetSheetName(defaultSheet, firstName); err != nil {
		t.Fatalf("rename first sheet: %v", err)
	}
	if _, err := file.NewSheet(secondName); err != nil {
		t.Fatalf("create second sheet: %v", err)
	}
	for index, row := range rows {
		cell, err := excelize.CoordinatesToCellName(1, index+1)
		if err != nil {
			t.Fatalf("cell name: %v", err)
		}
		if err := file.SetSheetRow(secondName, cell, &row); err != nil {
			t.Fatalf("set sheet row: %v", err)
		}
	}
	buffer, err := file.WriteToBuffer()
	if err != nil {
		t.Fatalf("write workbook: %v", err)
	}
	return bytes.Clone(buffer.Bytes())
}
