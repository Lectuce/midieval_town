package game

import (
	"context"
	"database/sql"
	"fmt"
	"math"
)

func LoadCartCargo(
	ctx context.Context,
	db *sql.DB,
	worldID string,
	quantityKg int64,
	expectedRevision int64,
) (CargoLoadResult, error) {
	if worldID == "" {
		return CargoLoadResult{}, fmt.Errorf(
			"world id is required",
		)
	}

	if quantityKg <= 0 {
		return CargoLoadResult{}, fmt.Errorf(
			"load quantity must be positive",
		)
	}

	if expectedRevision < 1 {
		return CargoLoadResult{}, fmt.Errorf(
			"expected revision must be positive",
		)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return CargoLoadResult{}, fmt.Errorf(
			"begin cargo loading: %w", err,
		)
	}
	defer tx.Rollback()

	// 1. 检查世界版本并取得本次写入。
	// 这里只是事务内修改，后续失败会一起回滚。
	result, err := tx.ExecContext(ctx, `
		UPDATE worlds
		SET revision = revision + 1
		WHERE id = ? AND revision = ?
	`, worldID, expectedRevision)
	if err != nil {
		return CargoLoadResult{}, fmt.Errorf(
			"update loading world revision: %w", err,
		)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return CargoLoadResult{}, fmt.Errorf(
			"read loading world update result: %w", err,
		)
	}
	if affected != 1 {
		return CargoLoadResult{}, ErrWorldRevisionConflict
	}

	// 2. 读取当前货车。
	var cart Cart

	err = tx.QueryRowContext(ctx, `
		SELECT
			world_id, id, scene_id,
			position_x, position_y, floor,
			repair_status, capacity_kg, revision
		FROM carts
		WHERE world_id = ? AND id = ?
	`, worldID, AbandonedCartID).Scan(
		&cart.WorldID,
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
		return CargoLoadResult{}, fmt.Errorf(
			"load cargo cart: %w", err,
		)
	}

	if cart.RepairStatus != CartStatusReady {
		return CargoLoadResult{}, fmt.Errorf(
			"cart must be repaired before loading",
		)
	}

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
		return CargoLoadResult{}, fmt.Errorf(
			"check cart transport before loading: %w",
			err,
		)
	}
	if inTransit {
		return CargoLoadResult{}, fmt.Errorf(
			"cannot load cargo while cart is in transit",
		)
	}

	// 货车必须仍停在现场。
	// 0.01 只是坐标浮点误差容差，不是玩家交互距离。
	distance := math.Hypot(
		cart.Position.X-CartSiteX,
		cart.Position.Y-CartSiteY,
	)

	if cart.SceneID != CartSiteSceneID ||
		cart.Floor != CartSiteFloor ||
		math.IsNaN(distance) ||
		math.IsInf(distance, 0) ||
		distance > 0.01 {
		return CargoLoadResult{}, fmt.Errorf(
			"cart is not at the loading site",
		)
	}

	// 3. 读取现场和车上的铁矿库存。
	siteCargo := CargoStock{
		WorldID:    worldID,
		LocationID: CargoLocationCartSite,
		ItemID:     IronOreItemID,
	}

	cartCargo := CargoStock{
		WorldID:    worldID,
		LocationID: CargoLocationCart,
		ItemID:     IronOreItemID,
	}

	for _, stock := range []*CargoStock{&siteCargo, &cartCargo} {
		err := tx.QueryRowContext(ctx, `
			SELECT quantity_kg, revision
			FROM cargo_stocks
			WHERE world_id = ?
				AND location_id = ?
				AND item_id = ?
		`,
			stock.WorldID,
			stock.LocationID,
			stock.ItemID,
		).Scan(
			&stock.QuantityKg,
			&stock.Revision,
		)
		if err != nil {
			return CargoLoadResult{}, fmt.Errorf(
				"load cargo stock at %q: %w",
				stock.LocationID,
				err,
			)
		}
	}

	if siteCargo.QuantityKg < quantityKg {
		return CargoLoadResult{}, fmt.Errorf(
			"insufficient iron ore: available %d kg, requested %d kg",
			siteCargo.QuantityKg,
			quantityKg,
		)
	}

	// 4. 计算车上所有货物的总重量。
	// cargo_stocks 中的数量统一以千克表示。
	var loadedKg int64

	err = tx.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(quantity_kg), 0)
		FROM cargo_stocks
		WHERE world_id = ? AND location_id = ?
	`,
		worldID,
		CargoLocationCart,
	).Scan(&loadedKg)
	if err != nil {
		return CargoLoadResult{}, fmt.Errorf(
			"calculate cart load: %w", err,
		)
	}

	if loadedKg > cart.CapacityKg {
		return CargoLoadResult{}, fmt.Errorf(
			"cart inventory already exceeds its capacity",
		)
	}

	// 用减法计算剩余容量，避免直接相加溢出。
	if quantityKg > cart.CapacityKg-loadedKg {
		return CargoLoadResult{}, fmt.Errorf(
			"cart capacity exceeded: remaining %d kg, requested %d kg",
			cart.CapacityKg-loadedKg,
			quantityKg,
		)
	}

	// 5. 扣除现场库存。
	result, err = tx.ExecContext(ctx, `
		UPDATE cargo_stocks
		SET quantity_kg = quantity_kg - ?,
			revision = revision + 1
		WHERE world_id = ?
			AND location_id = ?
			AND item_id = ?
			AND revision = ?
			AND quantity_kg >= ?
	`,
		quantityKg,
		worldID,
		CargoLocationCartSite,
		IronOreItemID,
		siteCargo.Revision,
		quantityKg,
	)
	if err != nil {
		return CargoLoadResult{}, fmt.Errorf(
			"deduct site cargo: %w", err,
		)
	}

	affected, err = result.RowsAffected()
	if err != nil {
		return CargoLoadResult{}, fmt.Errorf(
			"read site cargo deduction result: %w", err,
		)
	}
	if affected != 1 {
		return CargoLoadResult{}, fmt.Errorf(
			"site cargo changed during loading",
		)
	}

	// 6. 增加车上库存。
	result, err = tx.ExecContext(ctx, `
		UPDATE cargo_stocks
		SET quantity_kg = quantity_kg + ?,
			revision = revision + 1
		WHERE world_id = ?
			AND location_id = ?
			AND item_id = ?
			AND revision = ?
	`,
		quantityKg,
		worldID,
		CargoLocationCart,
		IronOreItemID,
		cartCargo.Revision,
	)
	if err != nil {
		return CargoLoadResult{}, fmt.Errorf(
			"add cart cargo: %w", err,
		)
	}

	affected, err = result.RowsAffected()
	if err != nil {
		return CargoLoadResult{}, fmt.Errorf(
			"read cart cargo update result: %w", err,
		)
	}
	if affected != 1 {
		return CargoLoadResult{}, fmt.Errorf(
			"cart cargo changed during loading",
		)
	}

	// 7. 载货情况发生变化，货车版本也更新。
	result, err = tx.ExecContext(ctx, `
		UPDATE carts
		SET revision = revision + 1
		WHERE world_id = ?
			AND id = ?
			AND revision = ?
			AND repair_status = ?
	`,
		worldID,
		cart.ID,
		cart.Revision,
		CartStatusReady,
	)
	if err != nil {
		return CargoLoadResult{}, fmt.Errorf(
			"update loaded cart revision: %w", err,
		)
	}

	affected, err = result.RowsAffected()
	if err != nil {
		return CargoLoadResult{}, fmt.Errorf(
			"read loaded cart update result: %w", err,
		)
	}
	if affected != 1 {
		return CargoLoadResult{}, fmt.Errorf(
			"cart changed during loading",
		)
	}

	// 8. 全部成功后一起提交。
	if err := tx.Commit(); err != nil {
		return CargoLoadResult{}, fmt.Errorf(
			"commit cargo loading: %w", err,
		)
	}

	siteCargo.QuantityKg -= quantityKg
	siteCargo.Revision++

	cartCargo.QuantityKg += quantityKg
	cartCargo.Revision++

	return CargoLoadResult{
		LoadedKg:      quantityKg,
		SiteCargo:     siteCargo,
		CartCargo:     cartCargo,
		WorldRevision: expectedRevision + 1,
	}, nil
}
