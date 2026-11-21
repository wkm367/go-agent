package analysis

// scope.go 把当前用户文档数据集封装进 context，
// 让 Agent 工具在调用链中切换到对应文档检索范围。
import "context"

// datasetContextKey 是数据集作用域在 context 中使用的私有键类型。
type datasetContextKey struct{}

const (
	datasetKindDocument = "document"
	datasetKindTable    = "table"
)

// DatasetScope 描述当前分析请求允许访问的用户数据集。
type DatasetScope struct {
	ID               string
	Name             string
	OriginalFilename string
	DocumentType     string
	ChunkCount       int
	Kind             string
	SheetName        string
	TableName        string
	RowCount         int
	Columns          []DatasetColumn
}

// IsTable 判断当前作用域是否是用户上传的 Excel 表。
func (s *DatasetScope) IsTable() bool {
	return s != nil && s.Kind == datasetKindTable
}

// datasetScopeFromDataset 把持久化数据集转换为 Agent 工具的访问边界。
func datasetScopeFromDataset(dataset *Dataset) *DatasetScope {
	if dataset == nil {
		return nil
	}
	scope := &DatasetScope{
		ID:               dataset.ID,
		Name:             dataset.Name,
		OriginalFilename: dataset.OriginalFilename,
		DocumentType:     dataset.DocumentType,
		ChunkCount:       dataset.ChunkCount,
		Kind:             datasetKindDocument,
	}
	if dataset.SourceType == datasetSourceExcel {
		scope.Kind = datasetKindTable
		scope.SheetName = dataset.SheetName
		scope.TableName = dataset.StorageTable
		scope.RowCount = dataset.StorageRows
		scope.Columns = append([]DatasetColumn(nil), dataset.Columns...)
	}
	return scope
}

// withDatasetScope 将数据集作用域写入 context；scope 为空时返回原 context。
func withDatasetScope(ctx context.Context, scope *DatasetScope) context.Context {
	if scope == nil {
		return ctx
	}
	return context.WithValue(ctx, datasetContextKey{}, scope)
}

// datasetScopeFromContext 从 context 读取有效的数据集作用域。
func datasetScopeFromContext(ctx context.Context) (*DatasetScope, bool) {
	scope, ok := ctx.Value(datasetContextKey{}).(*DatasetScope)
	return scope, ok && scope != nil
}
