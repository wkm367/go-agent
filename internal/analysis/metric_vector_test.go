package analysis

// metric_vector_test.go 验证指标向量文本、召回映射和关键词补位顺序。
import (
	"strings"
	"testing"
)

func TestMetricVectorDocumentsIncludeDescriptionAndAliases(t *testing.T) {
	documents := MetricVectorDocuments(DefaultCatalog())
	if len(documents) == 0 {
		t.Fatal("expected metric vector documents")
	}

	var refundRate bool
	for _, document := range documents {
		if document.ID != "refund_rate" {
			continue
		}
		refundRate = true
		if !strings.Contains(document.Content, "退款金额除以支付金额") {
			t.Fatalf("expected refund rate description, got %q", document.Content)
		}
		if !strings.Contains(document.Content, "退货率") {
			t.Fatalf("expected refund rate alias, got %q", document.Content)
		}
		if document.Metadata["type"] != "metric" {
			t.Fatalf("expected metric metadata, got %#v", document.Metadata)
		}
	}
	if !refundRate {
		t.Fatal("expected refund_rate vector document")
	}
}

func TestMetricsByNamesPreservesVectorRanking(t *testing.T) {
	catalog := DefaultCatalog()
	metrics := catalog.MetricsByNames([]string{"refund_rate", "missing", "gmv"})
	if len(metrics) != 2 {
		t.Fatalf("expected 2 metrics, got %d", len(metrics))
	}
	if metrics[0].Name != "refund_rate" || metrics[1].Name != "gmv" {
		t.Fatalf("unexpected metric order: %s, %s", metrics[0].Name, metrics[1].Name)
	}
}

func TestMergeMetricResultsAddsKeywordFallbackWithoutDuplicates(t *testing.T) {
	vectorResults := []SemanticMetric{
		{Name: "refund_rate"},
		{Name: "refund_amount"},
	}
	keywordResults := []SemanticMetric{
		{Name: "refund_amount"},
		{Name: "gmv"},
		{Name: "paid_gmv"},
	}

	merged := mergeMetricResults(vectorResults, keywordResults, 3)
	if len(merged) != 3 {
		t.Fatalf("expected 3 metrics, got %d", len(merged))
	}
	expected := []string{"refund_rate", "refund_amount", "gmv"}
	for i, name := range expected {
		if merged[i].Name != name {
			t.Fatalf("expected %s at position %d, got %s", name, i, merged[i].Name)
		}
	}
}
