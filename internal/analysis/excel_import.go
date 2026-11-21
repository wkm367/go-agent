package analysis

// excel_import.go 负责把用户上传的 Excel 工作表转换成
// 当前用户私有的 MySQL 表，并保存可审计的数据集元数据。
import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/xuri/excelize/v2"
)

const (
	// MaxExcelUploadBytes 与文档上传统一使用 20MB 上限。
	MaxExcelUploadBytes = MaxDocumentUploadBytes
	// MaxExcelRows 限制单个 Excel 数据集最多导入的数据行数。
	MaxExcelRows = 50000
	// MaxExcelColumns 限制单个 Excel 数据集最多导入的列数。
	MaxExcelColumns = 100
	// MaxExcelCellChars 限制单个单元格的字符数。
	MaxExcelCellChars = 32000
	// excelInsertBatchSize 是单次 INSERT 的最大行数。
	excelInsertBatchSize = 500
	// excelTablePrefix 是用户 Excel 表的固定前缀。
	excelTablePrefix = "user_dataset_"
)

// DatasetColumn 描述 Excel 表的一列及其原始表头。
type DatasetColumn struct {
	// Name 是写入 MySQL 的物理列名，例如 c1。
	Name string `json:"name"`
	// Header 是 Excel 第一行中的原始表头。
	Header string `json:"header"`
	// Type 是提供给模型的逻辑类型，例如 BIGINT、DOUBLE、DATETIME、TEXT。
	Type string `json:"type"`
}

type parsedExcelWorkbook struct {
	SheetName string
	Headers   []string
	Rows      [][]string
	Columns   []DatasetColumn
}

// CreateExcel 解析指定工作表、创建用户私有表并写入元数据。
func (s *DatasetService) CreateExcel(
	ctx context.Context,
	userID uint64,
	name string,
	filename string,
	sheet string,
	reader io.Reader,
) (*Dataset, error) {
	if strings.ToLower(filepath.Ext(filename)) != ".xlsx" {
		return nil, errors.New("当前只支持 .xlsx 文件")
	}
	data, err := readLimited(reader, MaxExcelUploadBytes)
	if err != nil {
		return nil, err
	}
	workbook, err := parseExcelWorkbook(data, sheet)
	if err != nil {
		return nil, err
	}

	contentHash := hashExcelContent(data, workbook.SheetName)
	duplicate, err := s.findDuplicateByContentHash(
		ctx,
		userID,
		datasetSourceExcel,
		contentHash,
	)
	if err != nil {
		return nil, err
	}
	if duplicate != nil {
		return nil, &DuplicateDatasetError{Existing: duplicate}
	}

	datasetID := uuid.NewString()
	tableName := excelTablePrefix + strings.ReplaceAll(datasetID, "-", "")
	ddl := buildExcelTableDDL(tableName, workbook.Columns)
	if err := s.db.WithContext(ctx).Exec(ddl).Error; err != nil {
		return nil, fmt.Errorf("create excel table: %w", err)
	}

	cleanup := true
	defer func() {
		if cleanup {
			_ = s.db.Exec("DROP TABLE IF EXISTS `" + tableName + "`").Error
		}
	}()

	if err := s.insertExcelRows(ctx, tableName, workbook.Columns, workbook.Rows); err != nil {
		return nil, err
	}

	name = limitRunes(strings.TrimSpace(name), 100)
	if name == "" {
		name = limitRunes(
			strings.TrimSuffix(filepath.Base(filename), filepath.Ext(filename)),
			100,
		)
	}
	if name == "" {
		name = "未命名 Excel"
	}

	columnsJSON, err := json.Marshal(workbook.Columns)
	if err != nil {
		return nil, fmt.Errorf("encode excel columns: %w", err)
	}
	dataset := &Dataset{
		ID:               datasetID,
		UserID:           userID,
		Name:             name,
		OriginalFilename: limitRunes(filepath.Base(filename), 255),
		SourceType:       datasetSourceExcel,
		DocumentType:     "xlsx",
		SheetName:        workbook.SheetName,
		StorageTable:     tableName,
		StorageRows:      len(workbook.Rows),
		StorageColumns:   string(columnsJSON),
		Columns:          workbook.Columns,
		ContentHash:      contentHash,
	}
	if err := s.db.WithContext(ctx).Create(dataset).Error; err != nil {
		return nil, fmt.Errorf("save excel dataset metadata: %w", err)
	}
	cleanup = false
	return dataset, nil
}

// parseExcelWorkbook 读取指定工作表；sheet 为空时选择第一个非空工作表。
func parseExcelWorkbook(data []byte, requestedSheet string) (*parsedExcelWorkbook, error) {
	file, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("解析 Excel 失败: %w", err)
	}
	defer file.Close()

	sheetName := strings.TrimSpace(requestedSheet)
	var rows [][]string
	if sheetName != "" {
		rows, err = file.GetRows(sheetName)
		if err != nil {
			return nil, fmt.Errorf("读取工作表 %q 失败: %w", sheetName, err)
		}
	} else {
		for _, candidate := range file.GetSheetList() {
			candidateRows, readErr := file.GetRows(candidate)
			if readErr != nil {
				continue
			}
			if hasNonEmptyExcelRow(candidateRows) {
				sheetName = candidate
				rows = candidateRows
				break
			}
		}
	}
	if sheetName == "" {
		return nil, errors.New("Excel 中没有可用的工作表")
	}

	headers, dataRows, err := normalizeExcelRows(rows)
	if err != nil {
		return nil, err
	}
	columns := make([]DatasetColumn, 0, len(headers))
	for index, header := range headers {
		columns = append(columns, DatasetColumn{
			Name:   fmt.Sprintf("c%d", index+1),
			Header: header,
			Type:   inferExcelColumnType(dataRows, index),
		})
	}
	return &parsedExcelWorkbook{
		SheetName: sheetName,
		Headers:   headers,
		Rows:      dataRows,
		Columns:   columns,
	}, nil
}

// normalizeExcelRows 定位表头、裁剪空列，并校验行列数量。
func normalizeExcelRows(rows [][]string) ([]string, [][]string, error) {
	headerIndex := -1
	for index, row := range rows {
		if hasNonEmptyExcelRow([][]string{row}) {
			headerIndex = index
			break
		}
	}
	if headerIndex < 0 {
		return nil, nil, errors.New("Excel 中没有可用数据")
	}

	maxColumns := 0
	for _, row := range rows[headerIndex:] {
		for index, value := range row {
			if strings.TrimSpace(value) != "" && index+1 > maxColumns {
				maxColumns = index + 1
			}
		}
	}
	if maxColumns == 0 {
		return nil, nil, errors.New("Excel 中没有可用数据")
	}
	if maxColumns > MaxExcelColumns {
		return nil, nil, fmt.Errorf("Excel 不能超过 %d 列", MaxExcelColumns)
	}

	headers := normalizeExcelHeaders(rows[headerIndex], maxColumns)
	dataRows := make([][]string, 0, len(rows)-headerIndex-1)
	for _, row := range rows[headerIndex+1:] {
		if !hasNonEmptyExcelRow([][]string{row}) {
			continue
		}
		normalized := make([]string, maxColumns)
		for index := 0; index < maxColumns && index < len(row); index++ {
			normalized[index] = strings.TrimSpace(row[index])
		}
		dataRows = append(dataRows, normalized)
		if len(dataRows) > MaxExcelRows {
			return nil, nil, fmt.Errorf("Excel 不能超过 %d 行数据", MaxExcelRows)
		}
	}
	if len(dataRows) == 0 {
		return nil, nil, errors.New("Excel 只有表头，没有数据行")
	}
	return headers, dataRows, nil
}

// normalizeExcelHeaders 为空表头补默认名称，并处理重复表头。
func normalizeExcelHeaders(row []string, count int) []string {
	headers := make([]string, count)
	seen := make(map[string]int, count)
	for index := 0; index < count; index++ {
		header := ""
		if index < len(row) {
			header = strings.TrimSpace(row[index])
		}
		if header == "" {
			header = fmt.Sprintf("列 %d", index+1)
		}
		header = limitRunes(header, 128)
		if previous := seen[header]; previous > 0 {
			seen[header] = previous + 1
			header = fmt.Sprintf("%s (%d)", header, previous+1)
		} else {
			seen[header] = 1
		}
		headers[index] = header
	}
	return headers
}

// inferExcelColumnType 根据非空单元格推断逻辑类型。
func inferExcelColumnType(rows [][]string, index int) string {
	hasValue := false
	allInteger := true
	allNumber := true
	allDate := true
	hasLeadingZero := false
	for _, row := range rows {
		if index >= len(row) {
			continue
		}
		value := strings.TrimSpace(row[index])
		if value == "" {
			continue
		}
		hasValue = true
		if _, ok := parseExcelInteger(value); !ok {
			allInteger = false
			if looksLikeLeadingZeroNumber(value) {
				hasLeadingZero = true
			}
		}
		if _, err := strconv.ParseFloat(value, 64); err != nil {
			allNumber = false
		}
		if _, err := parseExcelDate(value); err != nil {
			allDate = false
		}
	}
	switch {
	case !hasValue:
		return "TEXT"
	case hasLeadingZero:
		return "TEXT"
	case allInteger:
		return "BIGINT"
	case allNumber:
		return "DOUBLE"
	case allDate:
		return "DATETIME"
	default:
		return "TEXT"
	}
}

func looksLikeLeadingZeroNumber(value string) bool {
	unsigned := value
	if strings.HasPrefix(unsigned, "-") {
		unsigned = strings.TrimPrefix(unsigned, "-")
	}
	if len(unsigned) <= 1 || unsigned[0] != '0' {
		return false
	}
	for _, char := range unsigned[1:] {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}

func parseExcelInteger(value string) (int64, bool) {
	if len(value) > 1 {
		unsigned := value
		if unsigned[0] == '-' {
			unsigned = unsigned[1:]
		}
		if len(unsigned) > 1 && unsigned[0] == '0' {
			return 0, false
		}
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	return parsed, err == nil
}

func parseExcelDate(value string) (time.Time, error) {
	layouts := []string{
		"2006-01-02",
		"2006-1-2",
		"2006/01/02",
		"2006/1/2",
		"2006-01-02 15:04:05",
		"2006-1-2 15:04:05",
		"2006/01/02 15:04:05",
		"2006/1/2 15:04:05",
		"2006年01月02日",
		"2006年1月2日",
		time.RFC3339,
	}
	for _, layout := range layouts {
		if parsed, err := time.ParseInLocation(layout, value, time.Local); err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, errors.New("not a date")
}

// insertExcelRows 按批次把数据写入用户表。
func (s *DatasetService) insertExcelRows(
	ctx context.Context,
	tableName string,
	columns []DatasetColumn,
	rows [][]string,
) error {
	sqlDB, err := s.db.DB()
	if err != nil {
		return fmt.Errorf("get excel database connection: %w", err)
	}
	tx, err := sqlDB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin excel import: %w", err)
	}
	defer tx.Rollback()

	columnNames := make([]string, 0, len(columns))
	for _, column := range columns {
		columnNames = append(columnNames, "`"+column.Name+"`")
	}
	rowPlaceholder := "(" + strings.TrimSuffix(strings.Repeat("?,", len(columns)), ",") + ")"

	for start := 0; start < len(rows); start += excelInsertBatchSize {
		end := start + excelInsertBatchSize
		if end > len(rows) {
			end = len(rows)
		}
		groups := make([]string, 0, end-start)
		args := make([]any, 0, (end-start)*len(columns))
		for _, row := range rows[start:end] {
			groups = append(groups, rowPlaceholder)
			for index, column := range columns {
				value, err := convertExcelValue(row[index], column.Type)
				if err != nil {
					return fmt.Errorf(
						"第 %d 行列 %s 转换失败: %w",
						start+len(groups),
						column.Name,
						err,
					)
				}
				args = append(args, value)
			}
		}
		query := fmt.Sprintf(
			"INSERT INTO `%s` (%s) VALUES %s",
			tableName,
			strings.Join(columnNames, ", "),
			strings.Join(groups, ", "),
		)
		if _, err := tx.ExecContext(ctx, query, args...); err != nil {
			return fmt.Errorf("insert excel rows: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit excel import: %w", err)
	}
	return nil
}

func convertExcelValue(value, columnType string) (any, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	switch columnType {
	case "BIGINT":
		parsed, ok := parseExcelInteger(value)
		if !ok {
			return nil, errors.New("不是有效整数")
		}
		return parsed, nil
	case "DOUBLE":
		parsed, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return nil, errors.New("不是有效数字")
		}
		return parsed, nil
	case "DATETIME":
		parsed, err := parseExcelDate(value)
		if err != nil {
			return nil, errors.New("不是有效日期")
		}
		return parsed, nil
	default:
		if utf8.RuneCountInString(value) > MaxExcelCellChars {
			return nil, fmt.Errorf("单元格不能超过 %d 个字符", MaxExcelCellChars)
		}
		return value, nil
	}
}

func buildExcelTableDDL(tableName string, columns []DatasetColumn) string {
	definitions := make([]string, 0, len(columns))
	for _, column := range columns {
		definitions = append(definitions, fmt.Sprintf(
			"`%s` %s NULL",
			column.Name,
			excelSQLType(column.Type),
		))
	}
	return fmt.Sprintf(
		"CREATE TABLE `%s` (%s) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4",
		tableName,
		strings.Join(definitions, ", "),
	)
}

func excelSQLType(columnType string) string {
	switch columnType {
	case "BIGINT":
		return "BIGINT"
	case "DOUBLE":
		return "DOUBLE"
	case "DATETIME":
		return "DATETIME"
	default:
		return "LONGTEXT"
	}
}

func hasNonEmptyExcelRow(rows [][]string) bool {
	for _, row := range rows {
		for _, value := range row {
			if strings.TrimSpace(value) != "" {
				return true
			}
		}
	}
	return false
}

func hashExcelContent(data []byte, sheetName string) string {
	hash := sha256.New()
	_, _ = hash.Write(data)
	_, _ = hash.Write([]byte{0})
	_, _ = hash.Write([]byte(sheetName))
	return fmt.Sprintf("%x", hash.Sum(nil))
}

func isValidUserDatasetTable(tableName string) bool {
	if !strings.HasPrefix(tableName, excelTablePrefix) {
		return false
	}
	suffix := strings.TrimPrefix(tableName, excelTablePrefix)
	if len(suffix) != 32 {
		return false
	}
	for _, char := range suffix {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return false
		}
	}
	return true
}
