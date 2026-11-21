# 电商经营分析语义层

## 数据范围

样例库覆盖最近六个月，包含客户、商品、渠道、地区、订单、订单明细、退款和库存快照。所有经营指标仅允许查询 `customers`、`products`、`channels`、`regions`、`orders`、`order_items`、`refunds`、`inventory_snapshots`。

## 订单状态

- `paid` 表示订单已经支付且未退款。
- `refunded` 表示订单发生过退款。
- `cancelled` 表示订单取消，计算 GMV、销量和订单量时必须排除。

## GMV

GMV 是未取消订单的商品成交金额，公式为 `SUM(order_items.gross_amount)`，需要关联 `orders` 并按 `orders.status <> 'cancelled'` 过滤。GMV 不扣除退款。

## 支付金额

支付金额反映实际收款规模，公式为 `SUM(orders.paid_amount)`，只统计 `paid` 和 `refunded` 状态。同比或环比分析优先使用支付金额。

## 订单量与支付订单量

订单量统计未取消订单，公式为 `COUNT(DISTINCT orders.id)`。支付订单量统计 `paid` 和 `refunded` 状态订单。

## 客单价

客单价 AOV 等于支付金额除以支付订单量，必须使用 `NULLIF(支付订单量, 0)` 防止除零。

## 退款金额与退款率

退款金额是 `SUM(refunds.refund_amount)`。退款率等于退款金额除以支付金额，也使用 `NULLIF(支付金额, 0)` 保护。退款原因包括七天无理由、商品破损、描述不符、尺码不合适和物流超时。

## 销量

销量是 `SUM(order_items.quantity)`，排除取消订单。

## 毛利

毛利等于商品成交金额减去商品成本，再扣除退款金额，即 `SUM(order_items.gross_amount - order_items.cost_amount) - refund_amount`。

## 折扣率

折扣率等于订单优惠金额除以商品原始成交金额，公式为 `SUM(orders.discount_amount) / NULLIF(SUM(order_items.gross_amount), 0)`。

## 取消率

取消率等于统计期内取消订单数除以全部订单数，公式为 `SUM(CASE WHEN orders.status = 'cancelled' THEN 1 ELSE 0 END) / NULLIF(COUNT(orders.id), 0)`。取消率用于衡量订单流失情况，可按日期、渠道和地区拆解。

## 新客占比

新客占比等于统计期内首次支付日期落在统计期的客户数，除以统计期支付客户数。

## 复购率

复购率等于统计期内支付订单数大于等于二的客户数，除以支付客户数。

## 归因维度

经营问题可以从日期、渠道、地区、商品品类、商品和退款原因拆解。渠道包括自然流量、搜索广告、信息流、直播间、私域社群、达人分销、小程序和线下门店。

## 分析规范

- 先明确指标、时间范围和对比基期。
- 先看总体变化，再按维度拆解贡献。
- 结论必须引用查询结果，不得编造数字。
- 查询必须使用只读 SELECT，最多返回五百行。
- 报告需要说明数据限制和可能的替代解释。
