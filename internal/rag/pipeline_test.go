package rag

// pipeline_test.go 覆盖 Markdown 切块、Embedding API 类型选择
// 和 Milvus collection 向量维度生成。
import (
	"context"
	"strings"
	"testing"

	arkembed "github.com/cloudwego/eino-ext/components/embedding/ark"
	"github.com/milvus-io/milvus-sdk-go/v2/entity"
)

func TestSplitMarkdownDocument(t *testing.T) {
	docs, err := SplitMarkdownDocument(context.Background(), "../../knowledge/ecommerce_semantics.md")
	if err != nil {
		t.Fatalf("SplitMarkdownDocument failed: %v", err)
	}
	if len(docs) == 0 {
		t.Fatal("expected split documents, got none")
	}
	for i, doc := range docs {
		if doc.ID == "" {
			t.Fatalf("document %d has empty ID", i)
		}
		if strings.TrimSpace(doc.Content) == "" {
			t.Fatalf("document %d has empty content", i)
		}
	}
}

func TestResolveEmbeddingAPIType(t *testing.T) {
	t.Setenv("EMBEDDER_API_TYPE", "")
	if got := resolveEmbeddingAPIType("doubao-embedding-vision-251215"); got != arkembed.APITypeMultiModal {
		t.Fatalf("expected vision model to use multi-modal API, got %s", got)
	}
	if got := resolveEmbeddingAPIType("doubao-embedding-text"); got != arkembed.APITypeText {
		t.Fatalf("expected text model to use text API, got %s", got)
	}

	t.Setenv("EMBEDDER_API_TYPE", "text")
	if got := resolveEmbeddingAPIType("doubao-embedding-vision-251215"); got != arkembed.APITypeText {
		t.Fatalf("expected explicit text override, got %s", got)
	}
}

func TestCollectionFieldsUseDetectedDimension(t *testing.T) {
	fields := collectionFields(2048)
	for _, field := range fields {
		if field.Name == vectorField {
			expected := "65536"
			if field.TypeParams[entity.TypeParamDim] != expected {
				t.Fatalf(
					"expected binary vector dimension %s, got %s",
					expected,
					field.TypeParams[entity.TypeParamDim],
				)
			}
			return
		}
	}
	t.Fatal("vector field not found")
}

func TestUnrestrictedSearchParamDoesNotApplyDistanceThreshold(t *testing.T) {
	param := unrestrictedSearchParam()
	if param == nil {
		t.Fatal("expected search param")
	}
	if _, ok := param.Params()["range_filter"]; ok {
		t.Fatal("expected vector retrieval to keep the nearest TopK without a threshold")
	}
}
