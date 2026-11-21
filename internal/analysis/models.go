package analysis

// models.go 定义电商分析库的 GORM 业务模型，
// 对应客户、商品、渠道、地区、订单、退款和库存等数据表。
import "time"

// Customer 是客户维度表 customers 的持久化模型。
type Customer struct {
	ID           uint64    `gorm:"primaryKey"`
	CustomerName string    `gorm:"type:varchar(64);not null"`
	RegisteredAt time.Time `gorm:"not null;index"`
}

// TableName 指定 GORM 使用的客户表名。
func (Customer) TableName() string {
	return "customers"
}

// Product 是商品维度表 products 的持久化模型。
type Product struct {
	ID          uint64  `gorm:"primaryKey"`
	ProductName string  `gorm:"type:varchar(128);not null"`
	Category    string  `gorm:"type:varchar(64);not null;index"`
	SalePrice   float64 `gorm:"type:decimal(12,2);not null"`
	CostPrice   float64 `gorm:"type:decimal(12,2);not null"`
}

// TableName 指定 GORM 使用的商品表名。
func (Product) TableName() string {
	return "products"
}

// Channel 是销售渠道维度表 channels 的持久化模型。
type Channel struct {
	ID          uint64  `gorm:"primaryKey"`
	ChannelName string  `gorm:"type:varchar(64);not null;uniqueIndex"`
	FeeRate     float64 `gorm:"type:decimal(8,4);not null"`
}

// TableName 指定 GORM 使用的渠道表名。
func (Channel) TableName() string {
	return "channels"
}

// Region 是地区维度表 regions 的持久化模型。
type Region struct {
	ID         uint64 `gorm:"primaryKey"`
	RegionName string `gorm:"type:varchar(64);not null;uniqueIndex"`
}

// TableName 指定 GORM 使用的地区表名。
func (Region) TableName() string {
	return "regions"
}

// Order 是订单主表 orders 的持久化模型。
type Order struct {
	ID             uint64    `gorm:"primaryKey"`
	CustomerID     uint64    `gorm:"not null;index"`
	ChannelID      uint64    `gorm:"not null;index"`
	RegionID       uint64    `gorm:"not null;index"`
	OrderDate      time.Time `gorm:"not null;index"`
	Status         string    `gorm:"type:varchar(16);not null;index"`
	PaidAmount     float64   `gorm:"type:decimal(14,2);not null"`
	DiscountAmount float64   `gorm:"type:decimal(14,2);not null"`
}

// TableName 指定 GORM 使用的订单表名。
func (Order) TableName() string {
	return "orders"
}

// OrderItem 是订单商品明细表 order_items 的持久化模型。
type OrderItem struct {
	ID          uint64  `gorm:"primaryKey"`
	OrderID     uint64  `gorm:"not null;index"`
	ProductID   uint64  `gorm:"not null;index"`
	Quantity    int     `gorm:"not null"`
	UnitPrice   float64 `gorm:"type:decimal(12,2);not null"`
	GrossAmount float64 `gorm:"type:decimal(14,2);not null"`
	CostAmount  float64 `gorm:"type:decimal(14,2);not null"`
}

// TableName 指定 GORM 使用的订单明细表名。
func (OrderItem) TableName() string {
	return "order_items"
}

// Refund 是退款记录表 refunds 的持久化模型。
type Refund struct {
	ID           uint64    `gorm:"primaryKey"`
	OrderID      uint64    `gorm:"not null;uniqueIndex"`
	RefundAmount float64   `gorm:"type:decimal(14,2);not null"`
	Reason       string    `gorm:"type:varchar(64);not null;index"`
	RefundedAt   time.Time `gorm:"not null;index"`
}

// TableName 指定 GORM 使用的退款表名。
func (Refund) TableName() string {
	return "refunds"
}

// InventorySnapshot 是每日库存快照表 inventory_snapshots 的持久化模型。
type InventorySnapshot struct {
	ID           uint64    `gorm:"primaryKey"`
	ProductID    uint64    `gorm:"not null;index:idx_inventory_product_date,priority:1"`
	SnapshotDate time.Time `gorm:"type:date;not null;index:idx_inventory_product_date,priority:2"`
	Stock        int       `gorm:"not null"`
	InboundQty   int       `gorm:"not null"`
	OutboundQty  int       `gorm:"not null"`
}

// TableName 指定 GORM 使用的库存快照表名。
func (InventorySnapshot) TableName() string {
	return "inventory_snapshots"
}
