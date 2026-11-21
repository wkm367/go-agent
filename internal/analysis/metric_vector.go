package analysis

// metric_vector.go 将结构化指标目录转换为向量文档，并提供向量召回后的目录映射。
import (
	"strings"

	"github.com/yzletter/go-agentery/internal/rag"
)

// MetricVectorDocuments 将指标名称、描述、别名、公式和维度拼成可向量化文本。
func MetricVectorDocuments(catalog *SemanticCatalog) []rag.VectorDocument {
	if catalog == nil {
		return nil
	}
	documents := make([]rag.VectorDocument, 0, len(catalog.Metrics))
	for _, item := range catalog.Metrics {
		documents = append(documents, rag.VectorDocument{
			ID:      item.Name,
			Content: metricVectorText(item),
			Metadata: map[string]any{
				"type":         "metric",
				"name":         item.Name,
				"display_name": item.DisplayName,
				"description":  item.Description,
				"formula":      item.Formula,
				"aliases":      item.Aliases,
				"tables":       item.Tables,
				"dimensions":   item.Dimensions,
			},
		})
	}
	return documents
}

// MetricsByNames 按向量召回顺序映射回指标目录，并忽略不存在的 ID。
func (c *SemanticCatalog) MetricsByNames(names []string) []SemanticMetric {
	if c == nil || len(names) == 0 {
		return nil
	}
	index := make(map[string]SemanticMetric, len(c.Metrics))
	for _, item := range c.Metrics {
		index[strings.ToLower(strings.TrimSpace(item.Name))] = item
	}
	result := make([]SemanticMetric, 0, len(names))
	for _, name := range names {
		if item, ok := index[strings.ToLower(strings.TrimSpace(name))]; ok {
			result = append(result, item)
		}
	}
	return result
}

func metricVectorText(item SemanticMetric) string {
	aliases := strings.Join(item.Aliases, "、")
	tables := strings.Join(item.Tables, "、")
	dimensions := strings.Join(item.Dimensions, "、")
	return strings.TrimSpace(strings.Join([]string{
		"指标名称：" + item.DisplayName,
		"指标标识：" + item.Name,
		"指标描述：" + item.Description,
		"指标别名：" + aliases,
		"计算公式：" + item.Formula,
		"数据表：" + tables,
		"可用维度：" + dimensions,
	}, "\n"))
}

func mergeMetricResults(primary, fallback []SemanticMetric, topK int) []SemanticMetric {
	topK = normalizeMetricTopK(topK)
	merged := make([]SemanticMetric, 0, topK)
	seen := make(map[string]struct{}, topK)
	for _, metrics := range [][]SemanticMetric{primary, fallback} {
		for _, item := range metrics {
			name := strings.ToLower(strings.TrimSpace(item.Name))
			if name == "" {
				continue
			}
			if _, ok := seen[name]; ok {
				continue
			}
			seen[name] = struct{}{}
			merged = append(merged, item)
			if len(merged) == topK {
				return merged
			}
		}
	}
	return merged
}

func normalizeMetricTopK(topK int) int {
	if topK < 1 || topK > 12 {
		return 5
	}
	return topK
}
