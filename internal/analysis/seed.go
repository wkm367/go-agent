package analysis

// seed.go 生成可重复的电商演示数据，
// 负责业务表迁移、事务清空和历史订单快照批量灌数。
import (
	"context"
	"fmt"
	"math/rand"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// SeedOptions 控制演示数据的时间跨度和订单数量。
type SeedOptions struct {
	// Months 是订单回溯的月份数。
	Months int
	// Orders 是要生成的订单数量。
	Orders int
}

// SeedDemoData 迁移电商演示表，清空旧数据，并在单个事务中生成可重复的演示数据。
func SeedDemoData(ctx context.Context, dsn string, options SeedOptions) error {
	if options.Months < 1 {
		options.Months = 6
	}
	if options.Orders < 100 {
		options.Orders = 12000
	}

	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return fmt.Errorf("open analytics database: %w", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("get analytics connection pool: %w", err)
	}
	defer sqlDB.Close()

	if err := db.WithContext(ctx).AutoMigrate(
		&Customer{},
		&Product{},
		&Channel{},
		&Region{},
		&Order{},
		&OrderItem{},
		&Refund{},
		&InventorySnapshot{},
	); err != nil {
		return fmt.Errorf("migrate demo tables: %w", err)
	}

	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, table := range []string{
			"inventory_snapshots",
			"refunds",
			"order_items",
			"orders",
			"regions",
			"channels",
			"products",
			"customers",
		} {
			if err := tx.Exec("DELETE FROM " + table).Error; err != nil {
				return err
			}
		}
		return seedRows(tx, options)
	})
}

// seedRows 生成客户、商品、渠道、地区、订单、退款和库存快照。
func seedRows(tx *gorm.DB, options SeedOptions) error {
	rng := rand.New(rand.NewSource(20260912))
	now := time.Now()
	start := now.AddDate(0, -options.Months, 0)

	customers := make([]Customer, 0, 2500)
	for i := 1; i <= 2500; i++ {
		customers = append(customers, Customer{
			ID:           uint64(i),
			CustomerName: fmt.Sprintf("客户%04d", i),
			RegisteredAt: start.Add(time.Duration(rng.Intn(options.Months*30*24)) * time.Hour),
		})
	}
	if err := tx.CreateInBatches(customers, 500).Error; err != nil {
		return fmt.Errorf("seed customers: %w", err)
	}

	categories := []string{
		"数码家电", "手机通讯", "女装", "男装", "美妆个护",
		"食品饮料", "家居日用", "母婴用品", "运动户外", "宠物用品",
	}
	products := make([]Product, 0, 160)
	for i := 1; i <= 160; i++ {
		cost := 25 + rng.Float64()*650
		products = append(products, Product{
			ID:          uint64(i),
			ProductName: fmt.Sprintf("%s热销商品%03d", categories[(i-1)%len(categories)], i),
			Category:    categories[(i-1)%len(categories)],
			SalePrice:   roundMoney(cost * (1.35 + rng.Float64()*0.55)),
			CostPrice:   roundMoney(cost),
		})
	}
	if err := tx.CreateInBatches(products, 200).Error; err != nil {
		return fmt.Errorf("seed products: %w", err)
	}

	channelNames := []string{
		"自然流量", "搜索广告", "信息流", "直播间",
		"私域社群", "达人分销", "小程序", "线下门店",
	}
	channels := make([]Channel, 0, len(channelNames))
	for i, name := range channelNames {
		channels = append(channels, Channel{
			ID:          uint64(i + 1),
			ChannelName: name,
			FeeRate:     roundRate(0.015 + rng.Float64()*0.12),
		})
	}
	if err := tx.Create(&channels).Error; err != nil {
		return fmt.Errorf("seed channels: %w", err)
	}

	regionNames := []string{
		"广东", "浙江", "江苏", "上海", "北京", "山东",
		"四川", "湖北", "福建", "河南", "湖南", "安徽",
		"陕西", "重庆", "辽宁",
	}
	regions := make([]Region, 0, len(regionNames))
	for i, name := range regionNames {
		regions = append(regions, Region{ID: uint64(i + 1), RegionName: name})
	}
	if err := tx.Create(&regions).Error; err != nil {
		return fmt.Errorf("seed regions: %w", err)
	}

	orders := make([]Order, 0, options.Orders)
	orderItems := make([]OrderItem, 0, options.Orders*2)
	refunds := make([]Refund, 0, options.Orders/10)
	var orderItemID uint64 = 1
	var refundID uint64 = 1

	totalHours := int(now.Sub(start).Hours())
	for i := 1; i <= options.Orders; i++ {
		orderTime := start.Add(time.Duration(rng.Intn(totalHours)) * time.Hour)
		customerID := rng.Intn(len(customers)) + 1
		channelID := weightedChannel(rng)
		regionID := rng.Intn(len(regions)) + 1

		status := "paid"
		switch roll := rng.Float64(); {
		case roll < 0.04:
			status = "cancelled"
		case roll < 0.15:
			status = "refunded"
		}

		itemCount := 1
		if rng.Float64() < 0.34 {
			itemCount = 2
		}
		if rng.Float64() < 0.08 {
			itemCount = 3
		}

		var grossTotal float64
		var costTotal float64
		selectedProducts := make(map[uint64]struct{}, itemCount)
		for j := 0; j < itemCount; j++ {
			productIndex := rng.Intn(len(products))
			product := products[productIndex]
			if _, exists := selectedProducts[product.ID]; exists {
				j--
				continue
			}
			selectedProducts[product.ID] = struct{}{}
			quantity := 1
			if rng.Float64() < 0.22 {
				quantity = 2
			}
			unitPrice := roundMoney(product.SalePrice * (0.86 + rng.Float64()*0.18))
			gross := roundMoney(unitPrice * float64(quantity))
			cost := roundMoney(product.CostPrice * float64(quantity))
			grossTotal += gross
			costTotal += cost
			orderItems = append(orderItems, OrderItem{
				ID:          orderItemID,
				OrderID:     uint64(i),
				ProductID:   product.ID,
				Quantity:    quantity,
				UnitPrice:   unitPrice,
				GrossAmount: gross,
				CostAmount:  cost,
			})
			orderItemID++
		}

		discount := 0.0
		if rng.Float64() < 0.58 {
			discount = roundMoney(grossTotal * (0.02 + rng.Float64()*0.16))
		}
		paidAmount := roundMoney(grossTotal - discount)
		if status == "cancelled" {
			paidAmount = 0
		}
		if orderTime.Month() == now.AddDate(0, -1, 0).Month() && channelID == 3 {
			paidAmount = roundMoney(paidAmount * 0.82)
		}
		orders = append(orders, Order{
			ID:             uint64(i),
			CustomerID:     uint64(customerID),
			ChannelID:      uint64(channelID),
			RegionID:       uint64(regionID),
			OrderDate:      orderTime,
			Status:         status,
			PaidAmount:     paidAmount,
			DiscountAmount: discount,
		})

		if status == "refunded" {
			reason := []string{"七天无理由", "商品破损", "描述不符", "尺码不合适", "物流超时"}[rng.Intn(5)]
			refunds = append(refunds, Refund{
				ID:           refundID,
				OrderID:      uint64(i),
				RefundAmount: roundMoney(paidAmount * (0.45 + rng.Float64()*0.55)),
				Reason:       reason,
				RefundedAt:   orderTime.Add(time.Duration(1+rng.Intn(7)) * 24 * time.Hour),
			})
			refundID++
		}
	}

	if err := tx.CreateInBatches(orders, 500).Error; err != nil {
		return fmt.Errorf("seed orders: %w", err)
	}
	if err := tx.CreateInBatches(orderItems, 1000).Error; err != nil {
		return fmt.Errorf("seed order items: %w", err)
	}
	if err := tx.CreateInBatches(refunds, 500).Error; err != nil {
		return fmt.Errorf("seed refunds: %w", err)
	}

	inventory := make([]InventorySnapshot, 0, 120*30)
	var inventoryID uint64 = 1
	for productID := 1; productID <= 120; productID++ {
		stock := 80 + rng.Intn(400)
		for day := 29; day >= 0; day-- {
			inbound := rng.Intn(35)
			outbound := rng.Intn(42)
			stock += inbound - outbound
			if stock < 0 {
				stock = 0
			}
			inventory = append(inventory, InventorySnapshot{
				ID:           inventoryID,
				ProductID:    uint64(productID),
				SnapshotDate: now.AddDate(0, 0, -day),
				Stock:        stock,
				InboundQty:   inbound,
				OutboundQty:  outbound,
			})
			inventoryID++
		}
	}
	if err := tx.CreateInBatches(inventory, 1000).Error; err != nil {
		return fmt.Errorf("seed inventory: %w", err)
	}
	return nil
}

// weightedChannel 按预设权重返回销售渠道 ID，使演示订单分布更接近真实业务。
func weightedChannel(rng *rand.Rand) int {
	weights := []int{24, 18, 17, 14, 10, 8, 6, 3}
	roll := rng.Intn(100)
	total := 0
	for index, weight := range weights {
		total += weight
		if roll < total {
			return index + 1
		}
	}
	return 1
}

// roundMoney 将金额四舍五入到两位小数。
func roundMoney(value float64) float64 {
	return float64(int(value*100+0.5)) / 100
}

// roundRate 将费率四舍五入到四位小数。
func roundRate(value float64) float64 {
	return float64(int(value*10000+0.5)) / 10000
}
