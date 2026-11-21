package analysis

// catalog.go 定义内置电商语义目录，维护指标口径、别名、
// 可用维度、业务表结构，并提供关键词搜索和 schema 文本生成。
import (
	"fmt"
	"sort"
	"strings"
)

// SemanticMetric 定义指标的标准名称、别名、口径、公式和可用维度。
type SemanticMetric struct {
	Name        string   `json:"name"`
	DisplayName string   `json:"display_name"`
	Aliases     []string `json:"aliases"`
	Description string   `json:"description"`
	Formula     string   `json:"formula"`
	Tables      []string `json:"tables"`
	Dimensions  []string `json:"dimensions"`
}

// TableColumn 描述分析库中一个可查询字段。
type TableColumn struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description"`
}

// TableSchema 描述一张可查询业务表及其字段。
type TableSchema struct {
	Name        string        `json:"name"`
	Description string        `json:"description"`
	Columns     []TableColumn `json:"columns"`
}

// SemanticCatalog 汇总内置指标和表结构，供语义检索与提示词生成使用。
type SemanticCatalog struct {
	Metrics []SemanticMetric `json:"metrics"`
	Tables  []TableSchema    `json:"tables"`
}

// DefaultCatalog 返回内置电商分析语义目录。
func DefaultCatalog() *SemanticCatalog {
	return &SemanticCatalog{
		Metrics: []SemanticMetric{
			metric("gmv", "GMV", []string{"成交额", "交易额"}, "所有未取消订单的商品成交金额之和，不扣除退款。", "SUM(order_items.gross_amount)，排除 orders.status = 'cancelled'", []string{"orders", "order_items"}, []string{"date", "channel", "region", "category", "product"}),
			metric("paid_gmv", "支付金额", []string{"实收", "营收", "销售额"}, "订单实际支付金额之和，不扣除退款。", "SUM(orders.paid_amount)，orders.status IN ('paid','refunded')", []string{"orders"}, []string{"date", "channel", "region"}),
			metric("order_count", "订单量", []string{"订单数"}, "未取消订单数量。", "COUNT(DISTINCT orders.id)，排除 cancelled", []string{"orders"}, []string{"date", "channel", "region"}),
			metric("paid_order_count", "支付订单量", []string{"成单量"}, "已支付或已退款订单数量。", "COUNT(DISTINCT orders.id)，status IN ('paid','refunded')", []string{"orders"}, []string{"date", "channel", "region"}),
			metric("aov", "客单价", []string{"平均客单价", "AOV"}, "支付金额除以支付订单量。", "paid_gmv / NULLIF(paid_order_count, 0)", []string{"orders"}, []string{"date", "channel", "region"}),
			metric("refund_amount", "退款金额", []string{"退了多少"}, "退款记录金额之和。", "SUM(refunds.refund_amount)", []string{"refunds", "orders"}, []string{"date", "channel", "region", "reason"}),
			metric("refund_rate", "退款率", []string{"退货率"}, "退款金额除以支付金额。", "refund_amount / NULLIF(paid_gmv, 0)", []string{"orders", "refunds"}, []string{"date", "channel", "region", "reason"}),
			metric("units_sold", "销量", []string{"件数", "售出数量"}, "所有未取消订单的商品件数。", "SUM(order_items.quantity)，排除 cancelled", []string{"orders", "order_items"}, []string{"date", "category", "product", "channel"}),
			metric("gross_margin", "毛利", []string{"利润"}, "成交金额减去商品成本与退款金额。", "SUM(order_items.gross_amount - order_items.cost_amount) - refund_amount", []string{"orders", "order_items", "refunds"}, []string{"date", "category", "channel", "region"}),
			metric("discount_rate", "折扣率", []string{"优惠率"}, "优惠金额占原始商品金额比例。", "SUM(orders.discount_amount) / NULLIF(SUM(order_items.gross_amount), 0)", []string{"orders", "order_items"}, []string{"date", "channel", "category"}),
			metric("cancel_rate", "取消率", []string{"订单取消率", "取消订单占比"}, "统计期内取消订单数占全部订单数的比例。", "SUM(CASE WHEN orders.status = 'cancelled' THEN 1 ELSE 0 END) / NULLIF(COUNT(orders.id), 0)", []string{"orders"}, []string{"date", "channel", "region"}),
			metric("new_customer_ratio", "新客占比", []string{"新用户比例"}, "统计期内首次完成支付的客户数占支付客户数比例。", "首次支付日期落在统计期的客户数 / 支付客户数", []string{"orders", "customers"}, []string{"date", "channel", "region"}),
			metric("repeat_purchase_rate", "复购率", []string{"回购率"}, "统计期内支付订单数大于等于 2 的客户占支付客户数比例。", "COUNT(customer 支付订单数 >= 2) / 支付客户数", []string{"orders", "customers"}, []string{"date", "channel", "region"}),
		},
		Tables: []TableSchema{
			table("orders", "订单主表，一行代表一个订单。", []TableColumn{
				column("id", "bigint", "订单 ID"),
				column("customer_id", "bigint", "客户 ID"),
				column("channel_id", "bigint", "渠道 ID"),
				column("region_id", "bigint", "地区 ID"),
				column("order_date", "datetime", "下单时间"),
				column("status", "varchar", "paid、refunded 或 cancelled"),
				column("paid_amount", "decimal", "实付金额"),
				column("discount_amount", "decimal", "优惠金额"),
			}),
			table("order_items", "订单商品明细。", []TableColumn{
				column("id", "bigint", "明细 ID"),
				column("order_id", "bigint", "订单 ID"),
				column("product_id", "bigint", "商品 ID"),
				column("quantity", "int", "购买件数"),
				column("unit_price", "decimal", "成交单价"),
				column("gross_amount", "decimal", "商品成交金额"),
				column("cost_amount", "decimal", "商品成本"),
			}),
			table("customers", "客户维度表。", []TableColumn{
				column("id", "bigint", "客户 ID"),
				column("customer_name", "varchar", "客户名称"),
				column("registered_at", "datetime", "注册时间"),
			}),
			table("products", "商品维度表。", []TableColumn{
				column("id", "bigint", "商品 ID"),
				column("product_name", "varchar", "商品名称"),
				column("category", "varchar", "商品品类"),
				column("sale_price", "decimal", "标准售价"),
				column("cost_price", "decimal", "标准成本"),
			}),
			table("channels", "销售渠道维度表。", []TableColumn{
				column("id", "bigint", "渠道 ID"),
				column("channel_name", "varchar", "渠道名称"),
				column("fee_rate", "decimal", "渠道费率"),
			}),
			table("regions", "地区维度表。", []TableColumn{
				column("id", "bigint", "地区 ID"),
				column("region_name", "varchar", "省份或区域名称"),
			}),
			table("refunds", "退款记录表。", []TableColumn{
				column("id", "bigint", "退款 ID"),
				column("order_id", "bigint", "订单 ID"),
				column("refund_amount", "decimal", "退款金额"),
				column("reason", "varchar", "退款原因"),
				column("refunded_at", "datetime", "退款时间"),
			}),
			table("inventory_snapshots", "每日库存快照。", []TableColumn{
				column("id", "bigint", "快照 ID"),
				column("product_id", "bigint", "商品 ID"),
				column("snapshot_date", "date", "快照日期"),
				column("stock", "int", "库存数量"),
				column("inbound_qty", "int", "当日入库量"),
				column("outbound_qty", "int", "当日出库量"),
			}),
		},
	}
}

// Search 根据关键词对指标名称、别名、描述、表和维度进行轻量评分检索。
func (c *SemanticCatalog) Search(query string, topK int) []SemanticMetric {
	if c == nil {
		return nil
	}
	topK = normalizeMetricTopK(topK)
	query = strings.ToLower(strings.TrimSpace(query))
	type scoredMetric struct {
		score  int
		metric SemanticMetric
	}
	scored := make([]scoredMetric, 0, len(c.Metrics))
	for _, item := range c.Metrics {
		score := 0
		terms := append([]string{item.Name, item.DisplayName, item.Description}, item.Aliases...)
		terms = append(terms, item.Tables...)
		terms = append(terms, item.Dimensions...)
		for _, term := range terms {
			term = strings.ToLower(term)
			if term == "" {
				continue
			}
			if strings.Contains(query, term) {
				score += 10
			}
			for _, token := range strings.Fields(query) {
				if strings.Contains(term, token) {
					score += 3
				}
			}
		}
		if score > 0 {
			scored = append(scored, scoredMetric{score: score, metric: item})
		}
	}
	sort.SliceStable(scored, func(i, j int) bool {
		return scored[i].score > scored[j].score
	})
	if len(scored) > topK {
		scored = scored[:topK]
	}
	if len(scored) == 0 {
		metrics := append([]SemanticMetric(nil), c.Metrics...)
		if len(metrics) > topK {
			metrics = metrics[:topK]
		}
		return metrics
	}
	result := make([]SemanticMetric, 0, len(scored))
	for _, item := range scored {
		result = append(result, item.metric)
	}
	return result
}

// SchemaText 生成指定表的文本化结构说明；tables 为空时返回全部表。
func (c *SemanticCatalog) SchemaText(tables []string) string {
	if c == nil {
		return ""
	}
	allowed := make(map[string]struct{}, len(tables))
	for _, table := range tables {
		allowed[strings.TrimSpace(table)] = struct{}{}
	}

	var builder strings.Builder
	for _, schema := range c.Tables {
		if len(allowed) > 0 {
			if _, ok := allowed[schema.Name]; !ok {
				continue
			}
		}
		builder.WriteString("表 ")
		builder.WriteString(schema.Name)
		builder.WriteString("：")
		builder.WriteString(schema.Description)
		builder.WriteByte('\n')
		for _, column := range schema.Columns {
			fmt.Fprintf(&builder, "- %s %s：%s\n", column.Name, column.Type, column.Description)
		}
		builder.WriteByte('\n')
	}
	return strings.TrimSpace(builder.String())
}

// metric 是一个构造 SemanticMetric 的简写辅助函数。
func metric(
	name, displayName string,
	aliases []string,
	description, formula string,
	tables, dimensions []string,
) SemanticMetric {
	return SemanticMetric{
		Name:        name,
		DisplayName: displayName,
		Aliases:     aliases,
		Description: description,
		Formula:     formula,
		Tables:      tables,
		Dimensions:  dimensions,
	}
}

// table 是一个构造 TableSchema 的简写辅助函数。
func table(name, description string, columns []TableColumn) TableSchema {
	return TableSchema{Name: name, Description: description, Columns: columns}
}

// column 是一个构造 TableColumn 的简写辅助函数。
func column(name, dataType, description string) TableColumn {
	return TableColumn{Name: name, Type: dataType, Description: description}
}
