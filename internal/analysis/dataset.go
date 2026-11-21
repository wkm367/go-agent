package analysis

// dataset.go 管理用户上传文档的元数据、内容去重、向量索引和生命周期。
import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/yzletter/go-agentery/internal/rag"
	"gorm.io/gorm"
)

const (
	datasetSourceDocument = "document"
	datasetSourceExcel    = "excel"
)

// ErrDatasetNotFound 表示数据集不存在或不属于当前用户。
var ErrDatasetNotFound = errors.New("dataset not found")

// DuplicateDatasetError 表示同一用户已上传过内容完全相同的文档。
type DuplicateDatasetError struct {
	Existing *Dataset
}

// Error 返回用于日志和接口判断的重复数据集错误信息。
func (e *DuplicateDatasetError) Error() string {
	if e == nil || e.Existing == nil {
		return "dataset content already exists"
	}
	return fmt.Sprintf("dataset content already exists: %s", e.Existing.ID)
}

// Dataset 是用户上传文档的元数据记录。
type Dataset struct {
	ID               string          `gorm:"type:char(36);primaryKey" json:"id"`
	UserID           uint64          `gorm:"not null;index" json:"user_id"`
	Name             string          `gorm:"type:varchar(100);not null" json:"name"`
	OriginalFilename string          `gorm:"type:varchar(255);not null" json:"original_filename"`
	SourceType       string          `gorm:"type:varchar(32);not null;default:'';index" json:"source_type"`
	DocumentType     string          `gorm:"type:varchar(32);not null;default:''" json:"document_type"`
	ChunkCount       int             `gorm:"not null;default:0" json:"chunk_count"`
	CharacterCount   int             `gorm:"not null;default:0" json:"character_count"`
	SheetName        string          `gorm:"type:varchar(100);not null;default:''" json:"sheet_name,omitempty"`
	StorageTable     string          `gorm:"type:varchar(64);not null;default:''" json:"-"`
	StorageRows      int             `gorm:"not null;default:0" json:"row_count"`
	StorageColumns   string          `gorm:"type:mediumtext;not null" json:"-"`
	Columns          []DatasetColumn `gorm:"-" json:"columns,omitempty"`
	ContentHash      string          `gorm:"type:char(64);index" json:"-"`
	CreatedAt        time.Time       `json:"created_at"`
}

// TableName 指定 GORM 使用的数据集元数据表名。
func (Dataset) TableName() string {
	return "datasets"
}

// decodeColumns 把数据库中的列定义 JSON 还原成 API 可见结构。
func (d *Dataset) decodeColumns() error {
	d.Columns = nil
	if strings.TrimSpace(d.StorageColumns) == "" {
		return nil
	}
	if err := json.Unmarshal([]byte(d.StorageColumns), &d.Columns); err != nil {
		return fmt.Errorf("decode dataset columns: %w", err)
	}
	return nil
}

// DatasetService 管理文档元数据和 Milvus 文档向量。
type DatasetService struct {
	db *gorm.DB
}

// NewDatasetService 创建数据集服务。
func NewDatasetService(db *gorm.DB) *DatasetService {
	return &DatasetService{db: db}
}

// Migrate 创建或更新数据集元数据表。
func (s *DatasetService) Migrate(ctx context.Context) error {
	if err := s.db.WithContext(ctx).AutoMigrate(&Dataset{}); err != nil {
		return fmt.Errorf("migrate datasets table: %w", err)
	}
	for _, column := range []string{"table_name", "row_count", "columns_json"} {
		if err := dropDatasetLegacyColumn(ctx, s.db, column); err != nil {
			return err
		}
	}
	return nil
}

// Create 解析文档、检查同用户内容重复、切块并写入 Milvus。
func (s *DatasetService) Create(
	ctx context.Context,
	userID uint64,
	name string,
	filename string,
	reader io.Reader,
) (*Dataset, error) {
	document, err := parseDocument(filename, reader)
	if err != nil {
		return nil, err
	}
	chunks := chunkDocument(document.Text)
	if len(chunks) == 0 {
		return nil, errors.New("文档没有可用片段")
	}
	if len(chunks) > maxDocumentChunks {
		return nil, fmt.Errorf("文档切分后不能超过 %d 个片段", maxDocumentChunks)
	}

	contentHash := documentContentHash(document.DocumentType, document.Text)
	duplicate, err := s.findDuplicateByContentHash(
		ctx,
		userID,
		datasetSourceDocument,
		contentHash,
	)
	if err != nil {
		return nil, err
	}
	if duplicate != nil {
		return nil, &DuplicateDatasetError{Existing: duplicate}
	}

	datasetID := uuid.NewString()
	source := limitRunes(strings.TrimSpace(filepath.Base(filename)), 255)
	documentChunks := make([]rag.UserDocumentChunk, 0, len(chunks))
	for index, chunk := range chunks {
		documentChunks = append(documentChunks, rag.UserDocumentChunk{
			ID:         fmt.Sprintf("%s_%d", datasetID, index),
			DatasetID:  datasetID,
			UserID:     userID,
			Source:     source,
			ChunkIndex: index,
			Content:    chunk,
		})
	}
	if _, err := rag.IndexUserDocumentChunks(ctx, datasetID, userID, documentChunks); err != nil {
		return nil, fmt.Errorf("索引文档失败: %w", err)
	}

	cleanup := true
	defer func() {
		if cleanup {
			_ = rag.DeleteUserDocuments(context.Background(), datasetID)
		}
	}()

	name = limitRunes(strings.TrimSpace(name), 100)
	if name == "" {
		name = limitRunes(document.Title, 100)
	}
	if name == "" {
		name = "未命名文档"
	}

	dataset := &Dataset{
		ID:               datasetID,
		UserID:           userID,
		Name:             name,
		OriginalFilename: source,
		SourceType:       datasetSourceDocument,
		DocumentType:     document.DocumentType,
		ChunkCount:       len(chunks),
		CharacterCount:   len([]rune(document.Text)),
		ContentHash:      contentHash,
	}
	if err := s.db.WithContext(ctx).Create(dataset).Error; err != nil {
		return nil, fmt.Errorf("save dataset metadata: %w", err)
	}
	cleanup = false
	return dataset, nil
}

// Get 查询指定用户的数据集，文档和 Excel 数据集共用该入口。
func (s *DatasetService) Get(
	ctx context.Context,
	userID uint64,
	datasetID string,
) (*Dataset, error) {
	if _, err := uuid.Parse(datasetID); err != nil {
		return nil, ErrDatasetNotFound
	}
	var dataset Dataset
	if err := s.db.WithContext(ctx).
		Where(
			"id = ? AND user_id = ? AND source_type IN ?",
			datasetID,
			userID,
			[]string{datasetSourceDocument, datasetSourceExcel},
		).
		First(&dataset).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrDatasetNotFound
		}
		return nil, fmt.Errorf("get dataset: %w", err)
	}
	if err := dataset.decodeColumns(); err != nil {
		return nil, err
	}
	return &dataset, nil
}

// List 返回指定用户的文档和 Excel 数据集，按创建时间倒序排列。
func (s *DatasetService) List(ctx context.Context, userID uint64) ([]Dataset, error) {
	var datasets []Dataset
	if err := s.db.WithContext(ctx).
		Where(
			"user_id = ? AND source_type IN ?",
			userID,
			[]string{datasetSourceDocument, datasetSourceExcel},
		).
		Order("created_at DESC").
		Find(&datasets).Error; err != nil {
		return nil, fmt.Errorf("list datasets: %w", err)
	}
	for index := range datasets {
		if err := datasets[index].decodeColumns(); err != nil {
			return nil, err
		}
	}
	return datasets, nil
}

// Rename 修改文档显示名称，不改变已写入 Milvus 的内容。
func (s *DatasetService) Rename(
	ctx context.Context,
	userID uint64,
	datasetID string,
	name string,
) (*Dataset, error) {
	name = limitRunes(strings.TrimSpace(name), 100)
	if name == "" {
		return nil, errors.New("dataset name cannot be empty")
	}
	dataset, err := s.Get(ctx, userID, datasetID)
	if err != nil {
		return nil, err
	}
	if err := s.db.WithContext(ctx).
		Model(&Dataset{}).
		Where("id = ? AND user_id = ?", datasetID, userID).
		Update("name", name).Error; err != nil {
		return nil, fmt.Errorf("rename dataset: %w", err)
	}
	dataset.Name = name
	return dataset, nil
}

// Delete 删除数据集元数据，以及对应的 Milvus 向量或用户 Excel 表。
func (s *DatasetService) Delete(
	ctx context.Context,
	userID uint64,
	datasetID string,
) error {
	dataset, err := s.Get(ctx, userID, datasetID)
	if err != nil {
		return err
	}
	if dataset.SourceType == datasetSourceExcel {
		if !isValidUserDatasetTable(dataset.StorageTable) {
			return errors.New("invalid excel storage table")
		}
		if err := s.db.WithContext(ctx).Exec(
			"DROP TABLE IF EXISTS `" + dataset.StorageTable + "`",
		).Error; err != nil {
			return fmt.Errorf("drop excel dataset table: %w", err)
		}
	} else {
		if err := rag.DeleteUserDocuments(ctx, dataset.ID); err != nil {
			log.Printf("delete dataset vectors %s: %v", dataset.ID, err)
		}
	}
	if err := s.db.WithContext(ctx).
		Where("id = ? AND user_id = ?", datasetID, userID).
		Delete(&Dataset{}).Error; err != nil {
		return fmt.Errorf("delete dataset metadata: %w", err)
	}
	return nil
}

// findDuplicateByContentHash 在同一用户和同一数据源内查找内容完全相同的数据集。
func (s *DatasetService) findDuplicateByContentHash(
	ctx context.Context,
	userID uint64,
	sourceType string,
	contentHash string,
) (*Dataset, error) {
	if contentHash == "" {
		return nil, nil
	}
	var duplicate Dataset
	if err := s.db.WithContext(ctx).
		Where(
			"user_id = ? AND source_type = ? AND content_hash = ?",
			userID,
			sourceType,
			contentHash,
		).
		First(&duplicate).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("find duplicate dataset: %w", err)
	}
	if err := duplicate.decodeColumns(); err != nil {
		return nil, err
	}
	return &duplicate, nil
}

// limitRunes 按 Unicode 字符数截断字符串，避免切断多字节字符。
func limitRunes(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}

// dropDatasetLegacyColumn 删除旧 CSV 数据集留下的数据库列。
func dropDatasetLegacyColumn(ctx context.Context, db *gorm.DB, column string) error {
	var count int64
	if err := db.WithContext(ctx).Raw(
		`SELECT COUNT(*)
		 FROM information_schema.COLUMNS
		 WHERE TABLE_SCHEMA = DATABASE()
		   AND TABLE_NAME = ?
		   AND COLUMN_NAME = ?`,
		"datasets",
		column,
	).Scan(&count).Error; err != nil {
		return fmt.Errorf("check legacy dataset column %s: %w", column, err)
	}
	if count == 0 {
		return nil
	}
	statement := "ALTER TABLE `datasets` DROP COLUMN `" + column + "`"
	if err := db.WithContext(ctx).Exec(statement).Error; err != nil {
		return fmt.Errorf("drop legacy dataset column %s: %w", column, err)
	}
	return nil
}
