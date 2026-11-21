package rag

// pipeline.go 维护 Markdown 知识库和指标语义的 Milvus RAG 流水线，
// 负责文档切块、Ark Embedding、建索引、检索、重建和客户端缓存。
import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cloudwego/eino-ext/components/document/transformer/splitter/markdown"
	arkembed "github.com/cloudwego/eino-ext/components/embedding/ark"
	milvusindexer "github.com/cloudwego/eino-ext/components/indexer/milvus"
	milvusretriever "github.com/cloudwego/eino-ext/components/retriever/milvus"
	"github.com/cloudwego/eino/components/retriever"
	"github.com/cloudwego/eino/schema"
	"github.com/google/uuid"
	"github.com/milvus-io/milvus-sdk-go/v2/client"
	"github.com/milvus-io/milvus-sdk-go/v2/entity"
)

const (
	// CollectionName 是知识库向量在 Milvus 中使用的集合名。
	CollectionName = "AwesomeEino"
	// MetricCollectionName 是指标描述向量在 Milvus 中使用的集合名。
	MetricCollectionName = "SemanticMetrics"
	// UserDocumentCollectionName 是用户上传文档切块在 Milvus 中使用的集合名。
	UserDocumentCollectionName = "UserDocuments"
	// vectorField 是 Milvus 集合中的向量字段名。
	vectorField = "vector"
)

// VectorDocument 是写入 Milvus 的通用向量文档。
type VectorDocument struct {
	ID       string
	Content  string
	Metadata map[string]any
}

// UserDocumentChunk 是用户文档经过清洗和切块后的向量索引单元。
type UserDocumentChunk struct {
	ID         string
	DatasetID  string
	UserID     uint64
	Source     string
	ChunkIndex int
	Content    string
}

// 以下客户端都采用进程级单例，避免重复建立 Milvus 或 Embedding 连接。
var (
	clientMu  sync.Mutex
	milvusCli client.Client

	embedderMu sync.Mutex
	embedder   *arkembed.Embedder

	retrieverMu sync.Mutex
	milvusRag   retriever.Retriever

	metricRetrieverMu sync.Mutex
	metricMilvusRag   retriever.Retriever

	userDocumentRetrieverMu sync.Mutex
	userDocumentRag         retriever.Retriever
)

// SplitMarkdownDocument 使用 Eino Markdown Header Splitter 按标题切分知识文档。
func SplitMarkdownDocument(ctx context.Context, sourcePath string) ([]*schema.Document, error) {
	content, err := os.ReadFile(sourcePath)
	if err != nil {
		return nil, fmt.Errorf("read document %q: %w", sourcePath, err)
	}

	splitter, err := markdown.NewHeaderSplitter(ctx, &markdown.HeaderConfig{
		Headers: map[string]string{
			"#":   "h1",
			"##":  "h2",
			"###": "h3",
		},
		TrimHeaders: false,
	})
	if err != nil {
		return nil, fmt.Errorf("create markdown splitter: %w", err)
	}

	documentID := uuid.NewString()
	documents, err := splitter.Transform(ctx, []*schema.Document{
		{
			ID:      documentID,
			Content: string(content),
			MetaData: map[string]any{
				"source": filepath.Clean(sourcePath),
			},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("split markdown document: %w", err)
	}

	for i, doc := range documents {
		doc.ID = documentID + "_" + strconv.Itoa(i)
	}
	return documents, nil
}

// IndexDocument 将 document.md 分割后向量化并写入 Milvus 集合。
func IndexDocument(ctx context.Context, sourcePath string) (int, error) {
	documents, err := SplitMarkdownDocument(ctx, sourcePath)
	if err != nil {
		return 0, err
	}

	cli, err := GetMilvusClient(ctx)
	if err != nil {
		return 0, err
	}
	emb, err := GetArkEmbedder(ctx)
	if err != nil {
		return 0, err
	}
	vectorDimension, err := detectEmbeddingDimension(ctx, emb)
	if err != nil {
		return 0, err
	}

	idx, err := milvusindexer.NewIndexer(ctx, &milvusindexer.IndexerConfig{
		Client:     cli,
		Collection: CollectionName,
		Fields:     collectionFields(vectorDimension),
		Embedding:  emb,
	})
	if err != nil {
		return 0, fmt.Errorf("create milvus indexer: %w", err)
	}

	for _, doc := range documents {
		if _, err := idx.Store(ctx, []*schema.Document{doc}); err != nil {
			return 0, fmt.Errorf("store document %s: %w", doc.ID, err)
		}
	}
	return len(documents), nil
}

// IndexMetricDocuments 将指标语义文档向量化并写入独立的 Milvus 集合。
func IndexMetricDocuments(ctx context.Context, documents []VectorDocument) (int, error) {
	return indexVectorDocuments(
		ctx,
		MetricCollectionName,
		"semantic metric descriptions for vector retrieval",
		documents,
	)
}

// IndexUserDocumentChunks 将用户文档切块向量化并写入独立集合。
func IndexUserDocumentChunks(
	ctx context.Context,
	datasetID string,
	userID uint64,
	chunks []UserDocumentChunk,
) (int, error) {
	if strings.TrimSpace(datasetID) == "" {
		return 0, errors.New("dataset id is required for document indexing")
	}
	documents := make([]VectorDocument, 0, len(chunks))
	for index, chunk := range chunks {
		id := strings.TrimSpace(chunk.ID)
		if id == "" {
			id = fmt.Sprintf("%s_%d", datasetID, index)
		}
		documents = append(documents, VectorDocument{
			ID:      id,
			Content: chunk.Content,
			Metadata: map[string]any{
				"type":        "user_document",
				"dataset_id":  datasetID,
				"user_id":     userID,
				"source":      chunk.Source,
				"chunk_index": index,
			},
		})
	}
	return indexVectorDocuments(
		ctx,
		UserDocumentCollectionName,
		"user uploaded document chunks for dataset retrieval",
		documents,
	)
}

// indexVectorDocuments 复用统一的 Milvus Schema，批量写入不同业务的向量文档。
func indexVectorDocuments(
	ctx context.Context,
	collectionName, description string,
	documents []VectorDocument,
) (int, error) {
	if len(documents) == 0 {
		return 0, nil
	}

	cli, err := GetMilvusClient(ctx)
	if err != nil {
		return 0, err
	}
	emb, err := GetArkEmbedder(ctx)
	if err != nil {
		return 0, err
	}
	vectorDimension, err := detectEmbeddingDimension(ctx, emb)
	if err != nil {
		return 0, err
	}

	idx, err := milvusindexer.NewIndexer(ctx, &milvusindexer.IndexerConfig{
		Client:      cli,
		Collection:  collectionName,
		Description: description,
		Fields:      collectionFields(vectorDimension),
		Embedding:   emb,
	})
	if err != nil {
		return 0, fmt.Errorf("create milvus indexer for %s: %w", collectionName, err)
	}

	docs := make([]*schema.Document, 0, len(documents))
	for _, document := range documents {
		if strings.TrimSpace(document.ID) == "" {
			return 0, errors.New("vector document id is required")
		}
		if strings.TrimSpace(document.Content) == "" {
			return 0, fmt.Errorf("vector document %q has empty content", document.ID)
		}
		docs = append(docs, &schema.Document{
			ID:       document.ID,
			Content:  document.Content,
			MetaData: document.Metadata,
		})
	}
	if _, err := idx.Store(ctx, docs); err != nil {
		return 0, fmt.Errorf("store vector documents in %s: %w", collectionName, err)
	}
	return len(docs), nil
}

// RebuildMetricDocuments 删除旧指标集合并重建，确保固定指标 ID 不产生重复数据。
func RebuildMetricDocuments(ctx context.Context, documents []VectorDocument) (int, error) {
	if err := ResetMetricCollection(ctx); err != nil {
		return 0, err
	}
	return IndexMetricDocuments(ctx, documents)
}

// detectEmbeddingDimension 通过一次测试请求确定当前 Embedding 模型的向量维度。
func detectEmbeddingDimension(
	ctx context.Context,
	emb *arkembed.Embedder,
) (int, error) {
	vectors, err := emb.EmbedStrings(ctx, []string{"电商经营分析"})
	if err != nil {
		return 0, fmt.Errorf("detect embedding dimension: %w", err)
	}
	if len(vectors) == 0 || len(vectors[0]) == 0 {
		return 0, errors.New("embedding model returned an empty vector")
	}
	return len(vectors[0]), nil
}

// Retrieve 对用户问题做 Embedding，并从 Milvus 召回最相关的知识块。
func Retrieve(ctx context.Context, query string, topK int) ([]*schema.Document, error) {
	r, err := GetMilvusRetriever(ctx)
	if err != nil {
		return nil, err
	}
	return r.Retrieve(ctx, query, retriever.WithTopK(topK))
}

// RetrieveMetricIDs 按描述语义相似度召回指标 ID，并保持 Milvus 的距离排序。
func RetrieveMetricIDs(ctx context.Context, query string, topK int) ([]string, error) {
	r, err := GetMetricRetriever(ctx)
	if err != nil {
		return nil, err
	}
	documents, err := r.Retrieve(ctx, query, retriever.WithTopK(topK))
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(documents))
	for _, document := range documents {
		if document != nil && strings.TrimSpace(document.ID) != "" {
			ids = append(ids, document.ID)
		}
	}
	return ids, nil
}

// RetrieveUserDocuments 按当前数据集过滤并召回最相关的文档片段。
func RetrieveUserDocuments(
	ctx context.Context,
	datasetID, query string,
	topK int,
) ([]*schema.Document, error) {
	r, err := GetUserDocumentRetriever(ctx)
	if err != nil {
		return nil, err
	}
	return r.Retrieve(
		ctx,
		query,
		retriever.WithTopK(topK),
		milvusretriever.WithFilter(userDocumentFilter(datasetID)),
	)
}

// DeleteUserDocuments 删除指定数据集写入的文档向量。
func DeleteUserDocuments(ctx context.Context, datasetID string) error {
	cli, err := GetMilvusClient(ctx)
	if err != nil {
		return err
	}
	exists, err := cli.HasCollection(ctx, UserDocumentCollectionName)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	if err := cli.Delete(ctx, UserDocumentCollectionName, "", userDocumentFilter(datasetID)); err != nil {
		return fmt.Errorf("delete user document vectors: %w", err)
	}
	return cli.Flush(ctx, UserDocumentCollectionName, false)
}

// ResetCollection 删除现有 Milvus 集合，方便重建索引。
func ResetCollection(ctx context.Context) error {
	return resetCollection(ctx, CollectionName)
}

// ResetMetricCollection 删除现有指标向量集合，方便按最新目录重建。
func ResetMetricCollection(ctx context.Context) error {
	return resetCollection(ctx, MetricCollectionName)
}

func resetCollection(ctx context.Context, collectionName string) error {
	cli, err := GetMilvusClient(ctx)
	if err != nil {
		return err
	}
	exists, err := cli.HasCollection(ctx, collectionName)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	return cli.DropCollection(ctx, collectionName)
}

// GetMilvusClient 返回进程级 Milvus 客户端，并在首次调用时建立连接。
func GetMilvusClient(ctx context.Context) (client.Client, error) {
	clientMu.Lock()
	defer clientMu.Unlock()

	if milvusCli != nil {
		return milvusCli, nil
	}

	cli, err := client.NewClient(ctx, client.Config{
		Address:  envOr("MILVUS_ADDR", "127.0.0.1:19530"),
		Username: os.Getenv("MILVUS_USERNAME"),
		Password: os.Getenv("MILVUS_PASSWORD"),
	})
	if err != nil {
		return nil, fmt.Errorf("connect milvus: %w", err)
	}
	milvusCli = cli
	return milvusCli, nil
}

// GetArkEmbedder 返回进程级火山方舟 Embedding 实例。
func GetArkEmbedder(ctx context.Context) (*arkembed.Embedder, error) {
	embedderMu.Lock()
	defer embedderMu.Unlock()

	if embedder != nil {
		return embedder, nil
	}

	apiKey := os.Getenv("ARK_KEY")
	model := os.Getenv("EMBEDDER")
	if apiKey == "" || model == "" {
		return nil, fmt.Errorf("ARK_KEY and EMBEDDER are required for embedding")
	}

	apiType := resolveEmbeddingAPIType(model)
	timeout := 30 * time.Second
	emb, err := arkembed.NewEmbedder(ctx, &arkembed.EmbeddingConfig{
		APIKey:  apiKey,
		Model:   model,
		Timeout: &timeout,
		APIType: &apiType,
	})
	if err != nil {
		return nil, fmt.Errorf("create ark embedder: %w", err)
	}
	embedder = emb
	return embedder, nil
}

// resolveEmbeddingAPIType 根据显式配置或模型名称选择文本/多模态 Embedding API。
func resolveEmbeddingAPIType(model string) arkembed.APIType {
	configured := strings.ToLower(strings.TrimSpace(os.Getenv("EMBEDDER_API_TYPE")))
	switch configured {
	case "multi_modal", "multimodal", "vision":
		return arkembed.APITypeMultiModal
	case "text":
		return arkembed.APITypeText
	}
	if strings.Contains(strings.ToLower(model), "vision") {
		return arkembed.APITypeMultiModal
	}
	return arkembed.APITypeText
}

// GetMilvusRetriever 返回进程级 Milvus Retriever，并绑定当前 Embedding 实例。
func GetMilvusRetriever(ctx context.Context) (retriever.Retriever, error) {
	retrieverMu.Lock()
	defer retrieverMu.Unlock()

	if milvusRag != nil {
		return milvusRag, nil
	}

	r, err := newMilvusRetriever(ctx, CollectionName)
	if err != nil {
		return nil, err
	}
	milvusRag = r
	return milvusRag, nil
}

// GetMetricRetriever 返回指标描述向量检索器。
func GetMetricRetriever(ctx context.Context) (retriever.Retriever, error) {
	metricRetrieverMu.Lock()
	defer metricRetrieverMu.Unlock()

	if metricMilvusRag != nil {
		return metricMilvusRag, nil
	}

	r, err := newMilvusRetriever(ctx, MetricCollectionName)
	if err != nil {
		return nil, err
	}
	metricMilvusRag = r
	return metricMilvusRag, nil
}

// GetUserDocumentRetriever 返回用户文档向量检索器。
func GetUserDocumentRetriever(ctx context.Context) (retriever.Retriever, error) {
	userDocumentRetrieverMu.Lock()
	defer userDocumentRetrieverMu.Unlock()

	if userDocumentRag != nil {
		return userDocumentRag, nil
	}

	r, err := newMilvusRetriever(ctx, UserDocumentCollectionName)
	if err != nil {
		return nil, err
	}
	userDocumentRag = r
	return userDocumentRag, nil
}

func newMilvusRetriever(ctx context.Context, collectionName string) (retriever.Retriever, error) {
	cli, err := GetMilvusClient(ctx)
	if err != nil {
		return nil, err
	}
	emb, err := GetArkEmbedder(ctx)
	if err != nil {
		return nil, err
	}

	r, err := milvusretriever.NewRetriever(ctx, &milvusretriever.RetrieverConfig{
		Client:      cli,
		Collection:  collectionName,
		VectorField: vectorField,
		OutputFields: []string{
			"id",
			"content",
			"metadata",
		},
		TopK:      3,
		Sp:        unrestrictedSearchParam(),
		Embedding: emb,
	})
	if err != nil {
		return nil, fmt.Errorf("create milvus retriever for %s: %w", collectionName, err)
	}
	return r, nil
}

// unrestrictedSearchParam 不设置距离阈值，直接返回距离最近的 TopK 向量。
func unrestrictedSearchParam() entity.SearchParam {
	param, err := entity.NewIndexAUTOINDEXSearchParam(1)
	if err != nil {
		return nil
	}
	return param
}

// userDocumentFilter 使用 Metadata 中的 dataset_id 隔离不同用户的文档知识库。
func userDocumentFilter(datasetID string) string {
	escaped := strings.ReplaceAll(datasetID, `"`, `\"`)
	return fmt.Sprintf(`metadata["dataset_id"] == "%s"`, escaped)
}

// collectionFields 定义知识库集合的 ID、向量、正文和元数据字段。
// Milvus 二值向量以 bit 计维，因此这里将浮点维度乘以 32。
func collectionFields(vectorDimension int) []*entity.Field {
	return []*entity.Field{
		entity.NewField().
			WithName("id").
			WithIsPrimaryKey(true).
			WithDataType(entity.FieldTypeVarChar).
			WithMaxLength(255),
		entity.NewField().
			WithName(vectorField).
			WithDataType(entity.FieldTypeBinaryVector).
			WithDim(int64(vectorDimension * 32)),
		entity.NewField().
			WithName("content").
			WithDataType(entity.FieldTypeVarChar).
			WithMaxLength(8192),
		entity.NewField().
			WithName("metadata").
			WithDataType(entity.FieldTypeJSON),
	}
}

// envOr 返回环境变量值；未配置时返回 fallback。
func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
