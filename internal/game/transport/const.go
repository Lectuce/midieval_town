package transport

import "time"

const (
	CartStatusDamaged   = "damaged"
	CartStatusRepairing = "repairing"
	CartStatusReady     = "ready"

	AbandonedCartID = "abandoned_cart"
	IronOreItemID   = "iron_ore"

	// 三个不同的货物存放位置。
	CargoLocationCartSite = "abandoned_cart_site"
	CargoLocationCart     = "cart_abandoned_cart"
	CargoLocationMerchant = "merchant_warehouse"

	// 单位统一为千克。
	InitialIronOreKg    int64 = 100
	InitialCartCapacity int64 = 100

	CartSiteSceneID         = "town"
	CartSiteFloor           = 0
	CartSiteX       float64 = -5
	CartSiteY       float64 = -96

	WarehouseSceneID         = "town"
	WarehouseFloor           = 0
	WarehouseX       float64 = 472
	WarehouseY       float64 = -24

	TransportStatusInTransit = "in_transit"
	TransportStatusArrived   = "arrived"

	// 开发用固定路线耗时，由服务端决定。
	// 后续再结合路线和运输工具调整。
	CartTransportDuration = 20 * time.Minute
)
