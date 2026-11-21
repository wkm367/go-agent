package analysis

// tools.go 定义并实现 Agent 的四个工具：语义检索、表结构查询、
// 只读 SQL 和图表生成，同时负责参数解码与查询结果标准化。
import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cloudwego/eino/components/tool/utils"
	"github.com/cloudwego/eino/schema"
	"github.com/yzletter/go-agentery/internal/rag"
	"gorm.io/gorm"
)

const (
	// 以下常量是暴露给模型和事件日志的稳定工具名称。
	ToolSearchSemantics = "search_semantics"
	ToolGetSchema       = "get_schema"
	ToolRunReadonlySQL  = "run_readonly_sql"
	ToolCreateChart     = "create_chart"
)

// SemanticSearchInput 是语义检索工具的参数。
type SemanticSearchInput struct {
	Query string `json:"query" jsonschema:"required,description=用户问题或指标关键词"`
	TopK  int    `json:"top_k,omitempty" jsonschema:"description=返回指标数量，默认5，最大12"`
}

// SchemaInput 是表结构查询工具的参数。
type SchemaInput struct {
	Tables []string `json:"tables,omitempty" jsonschema:"description=需要查看的表名；为空时返回全部表结构"`
}

// SQLInput 是只读 SQL 工具的参数。
type SQLInput struct {
	SQL     string `json:"sql" jsonschema:"required,description=单条 MySQL SELECT 或 CTE 查询"`
	Purpose string `json:"purpose" jsonschema:"required,description=查询目的和对应分析步骤"`
}

// ChartInput 是图表生成工具的参数。
type ChartInput struct {
	Title       string           `json:"title" jsonschema:"required,description=图表标题"`
	ChartType   string           `json:"chart_type" jsonschema:"required,enum=line,enum=bar,enum=pie,description=图表类型"`
	XField      string           `json:"x_field" jsonschema:"required,description=横轴字段"`
	YFields     []string         `json:"y_fields" jsonschema:"required,description=纵轴数值字段列表"`
	Data        []map[string]any `json:"data" jsonschema:"required,description=图表数据，最多200行"`
	Description string           `json:"description,omitempty" jsonschema:"description=图表业务解释"`
}

// ChartSeries 是 ECharts 图表的一条数据系列。
type ChartSeries struct {
	Name string `json:"name"`
	Data []any  `json:"data"`
}

// ChartSpec 是保存到 Artifact 中的规范化图表描述。
type ChartSpec struct {
	Title       string        `json:"title"`
	ChartType   string        `json:"chart_type"`
	XField      string        `json:"x_field"`
	XValues     []any         `json:"x_values"`
	Series      []ChartSeries `json:"series"`
	Description string        `json:"description,omitempty"`
}

// ToolGateway 封装分析数据库、SQL 连接和语义目录，
// 为 Agent 提供统一的工具元信息与调用入口。
type ToolGateway struct {
	db      *gorm.DB
	sqlDB   *sql.DB
	catalog *SemanticCatalog
}

// NewToolGateway 创建工具网关；catalog 为空时使用内置语义目录。
func NewToolGateway(db *gorm.DB, catalog *SemanticCatalog) (*ToolGateway, error) {
	if db == nil {
		return nil, errors.New("analytics database is not configured")
	}
	if catalog == nil {
		catalog = DefaultCatalog()
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("get analytics sql database: %w", err)
	}
	return &ToolGateway{
		db:      db,
		sqlDB:   sqlDB,
		catalog: catalog,
	}, nil
}

// Catalog 返回当前工具网关使用的语义目录。
func (g *ToolGateway) Catalog() *SemanticCatalog {
	return g.catalog
}

// EinoToolInfos 返回内置电商数据模式下的四个工具描述。
func (g *ToolGateway) EinoToolInfos() ([]*schema.ToolInfo, error) {
	return g.EinoToolInfosForScope(nil)
}

// EinoToolInfosForScope 根据当前数据模式返回可用工具。
// 文档数据集只允许检索文档片段，不注册 SQL 和图表工具。
func (g *ToolGateway) EinoToolInfosForScope(scope *DatasetScope) ([]*schema.ToolInfo, error) {
	searchInfo, err := utils.GoStruct2ToolInfo[SemanticSearchInput](
		ToolSearchSemantics,
		"检索电商指标口径或用户上传文档的相关片段。分析问题前必须先调用。",
	)
	if err != nil {
		return nil, err
	}
	if scope != nil && !scope.IsTable() {
		return []*schema.ToolInfo{searchInfo}, nil
	}

	schemaInfo, err := utils.GoStruct2ToolInfo[SchemaInput](
		ToolGetSchema,
		"查看电商样例库的表和字段定义。只允许访问白名单业务表。",
	)
	if err != nil {
		return nil, err
	}
	sqlInfo, err := utils.GoStruct2ToolInfo[SQLInput](
		ToolRunReadonlySQL,
		"执行单条只读 SELECT 查询。禁止 CTE、写操作、系统库、注释和多语句；结果最多500行。",
	)
	if err != nil {
		return nil, err
	}
	chartInfo, err := utils.GoStruct2ToolInfo[ChartInput](
		ToolCreateChart,
		"根据已经查询到的数据生成 ECharts 图表产物。数据必须来自 run_readonly_sql 结果。",
	)
	if err != nil {
		return nil, err
	}
	if scope != nil {
		return []*schema.ToolInfo{schemaInfo, sqlInfo, chartInfo}, nil
	}
	return []*schema.ToolInfo{searchInfo, schemaInfo, sqlInfo, chartInfo}, nil
}

// Invoke 根据工具名解码参数并执行对应工具。
func (g *ToolGateway) Invoke(
	ctx context.Context,
	name string,
	argumentsJSON string,
) (*ToolResult, error) {
	switch name {
	case ToolSearchSemantics:
		var input SemanticSearchInput
		if err := decodeToolArguments(argumentsJSON, &input); err != nil {
			return nil, err
		}
		content, err := g.searchSemantics(ctx, input)
		return &ToolResult{Content: content}, err
	case ToolGetSchema:
		var input SchemaInput
		if err := decodeToolArguments(argumentsJSON, &input); err != nil {
			return nil, err
		}
		content := g.getSchemaForContext(ctx, input)
		return &ToolResult{Content: content}, nil
	case ToolRunReadonlySQL:
		var input SQLInput
		if err := decodeToolArguments(argumentsJSON, &input); err != nil {
			return nil, err
		}
		result, err := g.runReadonlySQL(ctx, input)
		if err != nil {
			return nil, err
		}
		content, err := json.Marshal(result)
		if err != nil {
			return nil, err
		}
		return &ToolResult{
			QueryResult: result,
			Content:     string(content),
		}, nil
	case ToolCreateChart:
		var input ChartInput
		if err := decodeToolArguments(argumentsJSON, &input); err != nil {
			return nil, err
		}
		content, err := g.createChart(input)
		return &ToolResult{Content: content}, err
	default:
		return nil, fmt.Errorf("unknown analytics tool %q", name)
	}
}

// searchSemantics 在内置模式下返回指标语义和知识；
// 在文档数据集模式下返回相似度最高的用户文档片段。
func (g *ToolGateway) searchSemantics(
	ctx context.Context,
	input SemanticSearchInput,
) (string, error) {
	if scope, ok := datasetScopeFromContext(ctx); ok {
		if scope.IsTable() {
			return "", fmt.Errorf(
				"Excel 数据集 %q 请使用 get_schema 和 run_readonly_sql 进行分析",
				scope.Name,
			)
		}
		topK := normalizeMetricTopK(input.TopK)
		chunks := make([]map[string]any, 0, topK)
		notice := "以下内容来自用户上传文档，只能依据这些片段回答，不得编造缺失信息。"
		if documents, err := rag.RetrieveUserDocuments(ctx, scope.ID, input.Query, topK); err == nil {
			for _, document := range documents {
				if document == nil || strings.TrimSpace(document.Content) == "" {
					continue
				}
				chunks = append(chunks, map[string]any{
					"content":     document.Content,
					"source":      documentMetadataString(document.MetaData, "source"),
					"chunk_index": documentMetadataInt(document.MetaData, "chunk_index"),
				})
			}
		} else {
			notice = "没有检索到匹配的文档片段，或文档向量库暂不可用。请明确告知用户当前文档中没有足够依据。"
		}
		data, err := json.Marshal(map[string]any{
			"dataset": map[string]any{
				"id":                scope.ID,
				"name":              scope.Name,
				"original_filename": scope.OriginalFilename,
				"document_type":     scope.DocumentType,
				"chunk_count":       scope.ChunkCount,
			},
			"chunks": chunks,
			"notice": notice,
		})
		if err != nil {
			return "", err
		}
		return string(data), nil
	}

	topK := normalizeMetricTopK(input.TopK)
	result, retrieval := g.searchMetrics(ctx, input.Query, topK)
	knowledge := make([]string, 0, 3)
	retrievalContext, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if documents, err := rag.Retrieve(retrievalContext, input.Query, 3); err == nil {
		for _, document := range documents {
			if document != nil && strings.TrimSpace(document.Content) != "" {
				knowledge = append(knowledge, document.Content)
			}
		}
	}
	data, err := json.Marshal(map[string]any{
		"metrics":   result,
		"knowledge": knowledge,
		"retrieval": retrieval,
		"notice":    "指标计算必须遵循 formula，并在生成 SQL 时保持状态过滤和 NULLIF 保护。",
	})
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// searchMetrics 优先按指标描述向量召回，并用关键词检索补足结果。
func (g *ToolGateway) searchMetrics(
	ctx context.Context,
	query string,
	topK int,
) ([]SemanticMetric, string) {
	keywordResults := g.catalog.Search(query, topK)

	vectorContext, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	names, err := rag.RetrieveMetricIDs(vectorContext, query, topK)
	if err != nil || len(names) == 0 {
		return keywordResults, "keyword_fallback"
	}

	vectorResults := g.catalog.MetricsByNames(names)
	if len(vectorResults) == 0 {
		return keywordResults, "keyword_fallback"
	}

	merged := mergeMetricResults(vectorResults, keywordResults, topK)
	if len(vectorResults) < len(merged) {
		return merged, "hybrid"
	}
	return merged, "vector"
}

// getSchemaForContext 根据上下文返回内置表结构或用户数据集结构。
func (g *ToolGateway) getSchemaForContext(
	ctx context.Context,
	input SchemaInput,
) string {
	if scope, ok := datasetScopeFromContext(ctx); ok {
		if scope.IsTable() {
			return tableDatasetContextText(scope)
		}
		return datasetContextText(scope)
	}
	return g.catalog.SchemaText(input.Tables)
}

// runReadonlySQL 在只读事务中执行经过白名单校验的 SELECT 查询。
func (g *ToolGateway) runReadonlySQL(
	ctx context.Context,
	input SQLInput,
) (*QueryResult, error) {
	allowedTables := allowedAnalyticsTables
	if scope, ok := datasetScopeFromContext(ctx); ok {
		if scope.IsTable() {
			if !isValidUserDatasetTable(scope.TableName) {
				return nil, errors.New("Excel 数据集存储表无效")
			}
			allowedTables = map[string]struct{}{scope.TableName: {}}
		} else {
			return nil, fmt.Errorf(
				"文档数据集 %q 不支持 SQL 查询，请使用 search_semantics 检索文档片段",
				scope.Name,
			)
		}
	}
	guardedSQL, err := ValidateReadonlySQLWithTables(input.SQL, allowedTables)
	if err != nil {
		return nil, err
	}

	started := time.Now()
	queryContext, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	tx, err := g.sqlDB.BeginTx(queryContext, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, fmt.Errorf("begin readonly transaction: %w", err)
	}
	defer tx.Rollback()

	rows, err := tx.QueryContext(queryContext, guardedSQL)
	if err != nil {
		return nil, fmt.Errorf("execute readonly SQL: %w", err)
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("read query columns: %w", err)
	}
	results := make([]map[string]any, 0, MaxQueryRows)
	truncated := false
	for rows.Next() {
		if len(results) >= MaxQueryRows {
			truncated = true
			break
		}
		values := make([]any, len(columns))
		pointers := make([]any, len(columns))
		for i := range values {
			pointers[i] = &values[i]
		}
		if err := rows.Scan(pointers...); err != nil {
			return nil, fmt.Errorf("scan query result: %w", err)
		}
		row := make(map[string]any, len(columns))
		for i, column := range columns {
			row[column] = normalizeSQLValue(values[i])
		}
		results = append(results, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate query result: %w", err)
	}
	if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
		return nil, fmt.Errorf("close readonly transaction: %w", err)
	}

	return &QueryResult{
		SQL:        input.SQL,
		Columns:    columns,
		Rows:       results,
		RowCount:   len(results),
		Truncated:  truncated,
		DurationMS: time.Since(started).Milliseconds(),
	}, nil
}

// createChart 校验图表参数并生成可持久化的 ChartSpec JSON。
func (g *ToolGateway) createChart(input ChartInput) (string, error) {
	input.Title = strings.TrimSpace(input.Title)
	input.ChartType = strings.ToLower(strings.TrimSpace(input.ChartType))
	input.XField = strings.TrimSpace(input.XField)
	if input.Title == "" || input.XField == "" {
		return "", errors.New("图表 title 和 x_field 不能为空")
	}
	if input.ChartType != "line" && input.ChartType != "bar" && input.ChartType != "pie" {
		return "", errors.New("chart_type 只支持 line、bar 或 pie")
	}
	if len(input.Data) == 0 || len(input.Data) > 200 {
		return "", errors.New("图表数据必须为 1 到 200 行")
	}
	if len(input.YFields) == 0 || len(input.YFields) > 8 {
		return "", errors.New("y_fields 必须包含 1 到 8 个字段")
	}

	spec := ChartSpec{
		Title:       input.Title,
		ChartType:   input.ChartType,
		XField:      input.XField,
		XValues:     make([]any, 0, len(input.Data)),
		Series:      make([]ChartSeries, 0, len(input.YFields)),
		Description: input.Description,
	}
	for _, row := range input.Data {
		spec.XValues = append(spec.XValues, row[input.XField])
	}
	for _, field := range input.YFields {
		series := ChartSeries{Name: field, Data: make([]any, 0, len(input.Data))}
		for _, row := range input.Data {
			series.Data = append(series.Data, row[field])
		}
		spec.Series = append(spec.Series, series)
	}
	data, err := json.Marshal(spec)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// decodeToolArguments 将模型生成的 JSON 参数解码到目标结构。
func decodeToolArguments(argumentsJSON string, target any) error {
	if strings.TrimSpace(argumentsJSON) == "" {
		argumentsJSON = "{}"
	}
	if err := json.Unmarshal([]byte(argumentsJSON), target); err != nil {
		return fmt.Errorf("decode tool arguments: %w", err)
	}
	return nil
}

// normalizeSQLValue 将数据库驱动返回的二进制和时间值转换为可 JSON 序列化的值。
func normalizeSQLValue(value any) any {
	switch typed := value.(type) {
	case []byte:
		return string(typed)
	case time.Time:
		return typed.Format(time.RFC3339)
	default:
		return typed
	}
}

// datasetContextText 生成文档数据集说明，提醒模型只能使用检索工具。
func datasetContextText(scope *DatasetScope) string {
	var builder strings.Builder
	builder.WriteString("当前文档数据集：")
	builder.WriteString(scope.Name)
	builder.WriteString("\n原始文件：")
	builder.WriteString(scope.OriginalFilename)
	builder.WriteString(fmt.Sprintf("\n文档类型：%s", scope.DocumentType))
	builder.WriteString(fmt.Sprintf("\n文档片段数：%d", scope.ChunkCount))
	builder.WriteString("\n该数据集没有 MySQL 表结构，也不允许执行 SQL。")
	builder.WriteString("\n请使用 search_semantics 检索文档片段，并仅依据检索结果回答。")
	return builder.String()
}

// tableDatasetContextText 生成 Excel 数据表的结构说明和查询边界。
func tableDatasetContextText(scope *DatasetScope) string {
	var builder strings.Builder
	builder.WriteString("当前 Excel 数据表：")
	builder.WriteString(scope.Name)
	builder.WriteString("\n原始文件：")
	builder.WriteString(scope.OriginalFilename)
	builder.WriteString("\n工作表：")
	builder.WriteString(scope.SheetName)
	builder.WriteString(fmt.Sprintf("\n数据行数：%d", scope.RowCount))
	builder.WriteString("\n物理表名：")
	builder.WriteString(scope.TableName)
	builder.WriteString("\n只允许查询这张表，不能访问其他表或系统表。")
	builder.WriteString("\n列定义：")
	for _, column := range scope.Columns {
		builder.WriteString(fmt.Sprintf(
			"\n- %s（原表头：%s，类型：%s）",
			column.Name,
			column.Header,
			column.Type,
		))
	}
	return builder.String()
}

// documentMetadataString 读取 Milvus JSON Metadata 中的字符串字段。
func documentMetadataString(metadata map[string]any, key string) string {
	if metadata == nil {
		return ""
	}
	switch value := metadata[key].(type) {
	case string:
		return value
	case []byte:
		return string(value)
	default:
		return ""
	}
}

// documentMetadataInt 读取 Milvus JSON Metadata 中的数值字段。
func documentMetadataInt(metadata map[string]any, key string) int {
	if metadata == nil {
		return 0
	}
	switch value := metadata[key].(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	default:
		return 0
	}
}
