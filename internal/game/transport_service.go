package game

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"time"
)

func StartCartTransport(
	ctx context.Context,
	db *sql.DB,
	worldID string,
	jobID string,
	expectedRevision int64,
) (TransportJob, error) {
	if worldID == "" || jobID == "" {
		return TransportJob{}, fmt.Errorf(
			"world id and transport job id are required",
		)
	}
	if expectedRevision < 1 {
		return TransportJob{}, fmt.Errorf(
			"expected revision must be positive",
		)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return TransportJob{}, fmt.Errorf(
			"begin cart transport: %w", err,
		)
	}
	defer tx.Rollback()

	// 1. 检查世界版本。
	result, err := tx.ExecContext(ctx, `
		UPDATE worlds
		SET revision = revision + 1
		WHERE id = ? AND revision = ?
	`, worldID, expectedRevision)
	if err != nil {
		return TransportJob{}, fmt.Errorf(
			"update transport world revision: %w", err,
		)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return TransportJob{}, fmt.Errorf(
			"read transport world update result: %w", err,
		)
	}
	if affected != 1 {
		return TransportJob{}, ErrWorldRevisionConflict
	}

	// 2. 读取货车状态。
	var cart Cart

	err = tx.QueryRowContext(ctx, `
		SELECT
			id, scene_id, position_x, position_y, floor,
			repair_status, capacity_kg, revision
		FROM carts
		WHERE world_id = ? AND id = ?
	`, worldID, AbandonedCartID).Scan(
		&cart.ID,
		&cart.SceneID,
		&cart.Position.X,
		&cart.Position.Y,
		&cart.Floor,
		&cart.RepairStatus,
		&cart.CapacityKg,
		&cart.Revision,
	)
	if err != nil {
		return TransportJob{}, fmt.Errorf(
			"load transport cart: %w", err,
		)
	}

	if cart.RepairStatus != CartStatusReady {
		return TransportJob{}, fmt.Errorf(
			"cart is not ready for transport",
		)
	}

	distance := math.Hypot(
		cart.Position.X-CartSiteX,
		cart.Position.Y-CartSiteY,
	)
	if cart.SceneID != CartSiteSceneID ||
		cart.Floor != CartSiteFloor ||
		math.IsNaN(distance) ||
		math.IsInf(distance, 0) ||
		distance > 0.01 {
		return TransportJob{}, fmt.Errorf(
			"cart is not at the departure site",
		)
	}

	// 3. 不能重复出发。
	var inTransit bool

	err = tx.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM transport_jobs
			WHERE world_id = ?
				AND cart_id = ?
				AND status = ?
		)
	`,
		worldID,
		cart.ID,
		TransportStatusInTransit,
	).Scan(&inTransit)
	if err != nil {
		return TransportJob{}, fmt.Errorf(
			"check active transport: %w", err,
		)
	}
	if inTransit {
		return TransportJob{}, fmt.Errorf(
			"cart is already in transit",
		)
	}

	// 4. 读取实际装载重量。
	var cargoKg int64

	err = tx.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(quantity_kg), 0)
		FROM cargo_stocks
		WHERE world_id = ? AND location_id = ?
	`,
		worldID,
		CargoLocationCart,
	).Scan(&cargoKg)
	if err != nil {
		return TransportJob{}, fmt.Errorf(
			"load transport cargo weight: %w", err,
		)
	}

	if cargoKg <= 0 {
		return TransportJob{}, fmt.Errorf(
			"cart has no cargo",
		)
	}
	if cargoKg > cart.CapacityKg {
		return TransportJob{}, fmt.Errorf(
			"cart is overloaded",
		)
	}

	// 5. 根据游戏时间计算预计到达时间。
	var rawGameTime string

	err = tx.QueryRowContext(ctx, `
		SELECT game_time
		FROM worlds
		WHERE id = ?
	`, worldID).Scan(&rawGameTime)
	if err != nil {
		return TransportJob{}, fmt.Errorf(
			"load transport game time: %w", err,
		)
	}

	startedAt, err := time.Parse(
		time.RFC3339Nano,
		rawGameTime,
	)
	if err != nil {
		return TransportJob{}, fmt.Errorf(
			"parse transport game time: %w", err,
		)
	}
	startedAt = startedAt.UTC()

	job := TransportJob{
		WorldID:             worldID,
		ID:                  jobID,
		CartID:              cart.ID,
		FromLocationID:      CargoLocationCartSite,
		ToLocationID:        CargoLocationMerchant,
		DestinationSceneID:  WarehouseSceneID,
		DestinationPosition: Position{X: WarehouseX, Y: WarehouseY},
		DestinationFloor:    WarehouseFloor,
		CargoKg:             cargoKg,
		Status:              TransportStatusInTransit,
		StartedAt:           startedAt,
		ArriveAt:            startedAt.Add(CartTransportDuration),
		Revision:            1,
	}

	if job.ArriveAt.Year() > 9999 {
		return TransportJob{}, fmt.Errorf(
			"arrival time is outside the supported range",
		)
	}

	// 6. 保存运输任务，货物继续留在车上。
	_, err = tx.ExecContext(ctx, `
		INSERT INTO transport_jobs (
			world_id, id, cart_id,
			from_location_id, to_location_id,
			destination_scene_id,
			destination_x, destination_y, destination_floor,
			cargo_kg, status, started_at, arrive_at, revision
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		job.WorldID,
		job.ID,
		job.CartID,
		job.FromLocationID,
		job.ToLocationID,
		job.DestinationSceneID,
		job.DestinationPosition.X,
		job.DestinationPosition.Y,
		job.DestinationFloor,
		job.CargoKg,
		job.Status,
		job.StartedAt.Format(time.RFC3339Nano),
		job.ArriveAt.Format(time.RFC3339Nano),
		job.Revision,
	)
	if err != nil {
		return TransportJob{}, fmt.Errorf(
			"create transport job: %w", err,
		)
	}

	// 出发也属于货车状态变化。
	result, err = tx.ExecContext(ctx, `
		UPDATE carts
		SET revision = revision + 1
		WHERE world_id = ? AND id = ? AND revision = ?
	`, worldID, cart.ID, cart.Revision)
	if err != nil {
		return TransportJob{}, fmt.Errorf(
			"update departing cart: %w", err,
		)
	}

	affected, err = result.RowsAffected()
	if err != nil {
		return TransportJob{}, fmt.Errorf(
			"read departing cart update result: %w", err,
		)
	}
	if affected != 1 {
		return TransportJob{}, fmt.Errorf(
			"cart changed before departure",
		)
	}

	if err := tx.Commit(); err != nil {
		return TransportJob{}, fmt.Errorf(
			"commit cart transport: %w", err,
		)
	}

	return job, nil
}
