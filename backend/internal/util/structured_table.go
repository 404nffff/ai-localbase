package util

import (
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/xuri/excelize/v2"
)

// StructuredTable 描述 CSV 文件或 XLSX 工作表中的表头和数据行。
type StructuredTable struct {
	FileName string
	Sheet    string
	Headers  []string
	Rows     []StructuredTableRow
}

// StructuredTableRow 保留过滤空行后的表内行号，便于回答中追溯数据位置。
type StructuredTableRow struct {
	Number int
	Values []string
}

// ExtractStructuredTables 按文件类型读取结构化表格，不经过向量检索或 LLM。
func ExtractStructuredTables(path string) ([]StructuredTable, error) {
	extension := strings.ToLower(filepath.Ext(path))
	switch extension {
	case ".csv":
		return extractStructuredCSV(path)
	case ".xlsx":
		return extractStructuredXLSX(path)
	default:
		return nil, fmt.Errorf("unsupported structured table type: %s", extension)
	}
}

func extractStructuredCSV(path string) ([]StructuredTable, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	reader := csv.NewReader(file)
	// 允许尾部缺列的实际业务 CSV，由表格构建阶段补齐空值。
	reader.FieldsPerRecord = -1
	records, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("read csv: %w", err)
	}

	table := buildStructuredTable(filepath.Base(path), "", records)
	if len(table.Headers) == 0 {
		return nil, nil
	}
	return []StructuredTable{table}, nil
}

func extractStructuredXLSX(path string) ([]StructuredTable, error) {
	workbook, err := excelize.OpenFile(path)
	if err != nil {
		return nil, fmt.Errorf("open xlsx: %w", err)
	}
	defer func() { _ = workbook.Close() }()

	tables := make([]StructuredTable, 0)
	for _, sheet := range workbook.GetSheetList() {
		rows, err := workbook.GetRows(sheet)
		if err != nil {
			return nil, fmt.Errorf("read xlsx sheet %s: %w", sheet, err)
		}
		table := buildStructuredTable(filepath.Base(path), sheet, rows)
		if len(table.Headers) == 0 {
			continue
		}
		tables = append(tables, table)
	}
	return tables, nil
}

// buildStructuredTable 过滤空行、规范空表头，并把缺失单元格补为空字符串。
func buildStructuredTable(fileName, sheet string, rows [][]string) StructuredTable {
	nonEmptyRows := make([][]string, 0, len(rows))
	for _, row := range rows {
		if !rowHasContent(row) {
			continue
		}
		nonEmptyRows = append(nonEmptyRows, trimTrailingEmptyCells(row))
	}
	if len(nonEmptyRows) == 0 {
		return StructuredTable{}
	}

	headers := normalizeTableHeaders(nonEmptyRows[0])
	tableRows := make([]StructuredTableRow, 0, len(nonEmptyRows)-1)
	for index, row := range nonEmptyRows[1:] {
		values := make([]string, len(headers))
		for cellIndex := range headers {
			if cellIndex < len(row) {
				values[cellIndex] = strings.TrimSpace(row[cellIndex])
			}
		}
		tableRows = append(tableRows, StructuredTableRow{
			Number: index + 2,
			Values: values,
		})
	}

	return StructuredTable{
		FileName: fileName,
		Sheet:    sheet,
		Headers:  headers,
		Rows:     tableRows,
	}
}
