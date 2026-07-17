package util

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/xuri/excelize/v2"
)

func TestExtractStructuredTablesCSVNormalizesRows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.csv")
	content := "姓名,,城市\n张三,100,上海\n\n李四,80\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write csv fixture: %v", err)
	}

	tables, err := ExtractStructuredTables(path)
	if err != nil {
		t.Fatalf("extract csv tables: %v", err)
	}
	if len(tables) != 1 {
		t.Fatalf("expected one csv table, got %d", len(tables))
	}
	table := tables[0]
	if len(table.Headers) != 3 || table.Headers[1] != "列2" {
		t.Fatalf("unexpected normalized headers: %#v", table.Headers)
	}
	if len(table.Rows) != 2 {
		t.Fatalf("expected two non-empty rows, got %#v", table.Rows)
	}
	if table.Rows[0].Number != 2 || table.Rows[1].Number != 3 {
		t.Fatalf("unexpected logical row numbers: %#v", table.Rows)
	}
	if len(table.Rows[1].Values) != 3 || table.Rows[1].Values[2] != "" {
		t.Fatalf("expected missing csv cell to be padded, got %#v", table.Rows[1].Values)
	}
}

func TestExtractStructuredTablesXLSXReadsMultipleSheets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "metrics.xlsx")
	workbook := excelize.NewFile()
	defaultSheet := workbook.GetSheetName(0)
	if err := workbook.SetSheetName(defaultSheet, "员工"); err != nil {
		t.Fatalf("rename default sheet: %v", err)
	}
	if err := workbook.SetSheetRow("员工", "A1", &[]any{"姓名", "薪资"}); err != nil {
		t.Fatalf("write employee headers: %v", err)
	}
	if err := workbook.SetSheetRow("员工", "A2", &[]any{"张三", 24000}); err != nil {
		t.Fatalf("write employee row: %v", err)
	}
	if _, err := workbook.NewSheet("部门"); err != nil {
		t.Fatalf("create department sheet: %v", err)
	}
	if err := workbook.SetSheetRow("部门", "A1", &[]any{"部门", "人数"}); err != nil {
		t.Fatalf("write department headers: %v", err)
	}
	if err := workbook.SetSheetRow("部门", "A2", &[]any{"研发", 8}); err != nil {
		t.Fatalf("write department row: %v", err)
	}
	if err := workbook.SaveAs(path); err != nil {
		t.Fatalf("save xlsx fixture: %v", err)
	}
	if err := workbook.Close(); err != nil {
		t.Fatalf("close xlsx fixture: %v", err)
	}

	tables, err := ExtractStructuredTables(path)
	if err != nil {
		t.Fatalf("extract xlsx tables: %v", err)
	}
	if len(tables) != 2 {
		t.Fatalf("expected two xlsx tables, got %#v", tables)
	}
	if tables[0].Sheet != "员工" || tables[0].Rows[0].Values[1] != "24000" {
		t.Fatalf("unexpected employee sheet: %#v", tables[0])
	}
	if tables[1].Sheet != "部门" || tables[1].Rows[0].Values[0] != "研发" {
		t.Fatalf("unexpected department sheet: %#v", tables[1])
	}
}

func TestExtractStructuredTablesRejectsUnsupportedFile(t *testing.T) {
	if _, err := ExtractStructuredTables(filepath.Join(t.TempDir(), "users.md")); err == nil {
		t.Fatal("expected unsupported structured table error")
	}
}
