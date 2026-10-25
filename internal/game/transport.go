package game

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
