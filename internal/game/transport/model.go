package game

import (
	"cybertown/internal/game/model"
	"time"
)

type Cart struct {
	WorldID      string         `json:"world_id"`
	ID           string         `json:"id"`
	SceneID      string         `json:"scene_id"`
	Position     model.Position `json:"position"`
	Floor        int            `json:"floor"`
	RepairStatus string         `json:"repair_status"`
	CapacityKg   int64          `json:"capacity_kg"`
	Revision     int64          `json:"revision"`
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

type CargoUnloadResult struct {
	UnloadedKg     int64      `json:"unloaded_kg"`
	CartCargo      CargoStock `json:"cart_cargo"`
	WarehouseCargo CargoStock `json:"warehouse_cargo"`
	WorldRevision  int64      `json:"world_revision"`
}

type TransportJob struct {
	WorldID string `json:"world_id"`
	ID      string `json:"id"`
	CartID  string `json:"cart_id"`

	FromLocationID string `json:"from_location_id"`
	ToLocationID   string `json:"to_location_id"`

	// 保存出发时确定的目的地，避免运行中修改配置影响已有任务。
	DestinationSceneID  string         `json:"destination_scene_id"`
	DestinationPosition model.Position `json:"destination_position"`
	DestinationFloor    int            `json:"destination_floor"`

	CargoKg int64 `json:"cargo_kg"`

	Status    string    `json:"status"`
	StartedAt time.Time `json:"started_at"`
	ArriveAt  time.Time `json:"arrive_at"`
	Revision  int64     `json:"revision"`
}
