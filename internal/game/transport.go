package game

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

type Cart struct {
	WorldID      string   `json:"world_id"`
	ID           string   `json:"id"`
	SceneID      string   `json:"scene_id"`
	Position     Position `json:"position"`
	Floor        int      `json:"floor"`
	RepairStatus string   `json:"repair_status"`
	CapacityKg   int64    `json:"capacity_kg"`
	Revision     int64    `json:"revision"`
}

type CargoStock struct {
	WorldID    string `json:"world_id"`
	LocationID string `json:"location_id"`
	ItemID     string `json:"item_id"`
	QuantityKg int64  `json:"quantity_kg"`
	Revision   int64  `json:"revision"`
}

type CargoLoadResult struct {
	LoadedKg      int64      `json:"loaded_kg"`
	SiteCargo     CargoStock `json:"site_cargo"`
	CartCargo     CargoStock `json:"cart_cargo"`
	WorldRevision int64      `json:"world_revision"`
}

type TransportJob struct {
	WorldID string `json:"world_id"`
	ID      string `json:"id"`
	CartID  string `json:"cart_id"`

	FromLocationID string `json:"from_location_id"`
	ToLocationID   string `json:"to_location_id"`

	// 保存出发时确定的目的地，避免运行中修改配置影响已有任务。
	DestinationSceneID  string   `json:"destination_scene_id"`
	DestinationPosition Position `json:"destination_position"`
	DestinationFloor    int      `json:"destination_floor"`

	CargoKg int64 `json:"cargo_kg"`

	Status    string    `json:"status"`
	StartedAt time.Time `json:"started_at"`
	ArriveAt  time.Time `json:"arrive_at"`
	Revision  int64     `json:"revision"`
}
