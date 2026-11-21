package main

// rag-index 是 RAG 建索引命令，负责按需重建 Milvus 集合，
// 并把 Markdown 知识文档和结构化指标描述向量化后写入向量库。
import (
	"context"
	"flag"
	"fmt"

	"github.com/yzletter/go-agentery/internal/analysis"
	"github.com/yzletter/go-agentery/internal/rag"
)

// main 解析文档路径和重建选项，然后执行 Milvus 建索引流程。
func main() {
	source := flag.String(
		"source",
		"./knowledge/ecommerce_semantics.md",
		"markdown document to index",
	)
	drop := flag.Bool("drop", false, "drop existing collection before indexing")
	flag.Parse()

	ctx := context.Background()
	if *drop {
		if err := rag.ResetCollection(ctx); err != nil {
			panic(fmt.Sprintf("reset collection: %v", err))
		}
	}

	count, err := rag.IndexDocument(ctx, *source)
	if err != nil {
		panic(fmt.Sprintf("index document: %v", err))
	}

	metricCount, err := rag.RebuildMetricDocuments(
		ctx,
		analysis.MetricVectorDocuments(analysis.DefaultCatalog()),
	)
	if err != nil {
		panic(fmt.Sprintf("index metric descriptions: %v", err))
	}
	fmt.Printf(
		"indexed %d chunks into %s and %d metric descriptions into %s\n",
		count,
		rag.CollectionName,
		metricCount,
		rag.MetricCollectionName,
	)
}
