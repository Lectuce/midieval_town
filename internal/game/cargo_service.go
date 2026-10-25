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

func UnloadCartCargo(
	ctx context.Context,
	db *sql.DB,
	worldID string,
	quantityKg int64,
	expectedRevision int64,
) (CargoUnloadResult, error) {
	if worldID == "" {
		return CargoUnloadResult{}, fmt.Errorf(
			"world id is required",
		)
	}
	if quantityKg <= 0 {
		return CargoUnloadResult{}, fmt.Errorf(
			"unload quantity must be positive",
		)
	}
	if expectedRevision < 1 {
		return CargoUnloadResult{}, fmt.Errorf(
			"expected revision must be positive",
		)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return CargoUnloadResult{}, fmt.Errorf(
			"begin cargo unloading: %w", err,
		)
	}
	defer tx.Rollback()

	// 1. 检查世界版本。
	// 后续失败时，这次版本更新也会回滚。
	result, err := tx.ExecContext(ctx, `
		UPDATE worlds
		SET revision = revision + 1
		WHERE id = ? AND revision = ?
	`, worldID, expectedRevision)
	if err != nil {
		return CargoUnloadResult{}, fmt.Errorf(
			"update unloading world revision: %w", err,
		)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return CargoUnloadResult{}, fmt.Errorf(
			"read unloading world update result: %w", err,
		)
	}
	if affected != 1 {
		return CargoUnloadResult{}, ErrWorldRevisionConflict
	}

	// 2. 读取货车实际位置。
	var cart Cart

	err = tx.QueryRowContext(ctx, `
		SELECT id, scene_id, position_x, position_y, floor, revision
		FROM carts
		WHERE world_id = ? AND id = ?
	`, worldID, AbandonedCartID).Scan(
		&cart.ID,
		&cart.SceneID,
		&cart.Position.X,
		&cart.Position.Y,
		&cart.Floor,
		&cart.Revision,
	)
	if err != nil {
		return CargoUnloadResult{}, fmt.Errorf(
			"load unloading cart: %w", err,
		)
	}

	// 货车必须停在仓库交付点。
	distance := math.Hypot(
		cart.Position.X-WarehouseX,
		cart.Position.Y-WarehouseY,
	)
	if cart.SceneID != WarehouseSceneID ||
		cart.Floor != WarehouseFloor ||
		math.IsNaN(distance) ||
		math.IsInf(distance, 0) ||
		distance > 0.01 {
		return CargoUnloadResult{}, fmt.Errorf(
			"cart is not at the warehouse",
		)
	}

	// 3. 检查没有进行中的运输，并且存在到仓库的到达记录。
	var inTransit, hasArrived bool

	err = tx.QueryRowContext(ctx, `
		SELECT
			EXISTS (
				SELECT 1
				FROM transport_jobs
				WHERE world_id = ?
					AND cart_id = ?
					AND status = ?
			),
			EXISTS (
				SELECT 1
				FROM transport_jobs
				WHERE world_id = ?
					AND cart_id = ?
					AND to_location_id = ?
					AND status = ?
			)
	`,
		worldID,
		cart.ID,
		TransportStatusInTransit,
		worldID,
		cart.ID,
		CargoLocationMerchant,
		TransportStatusArrived,
	).Scan(&inTransit, &hasArrived)
	if err != nil {
		return CargoUnloadResult{}, fmt.Errorf(
			"check unloading transport state: %w", err,
		)
	}

	if inTransit {
		return CargoUnloadResult{}, fmt.Errorf(
			"cannot unload cargo while cart is in transit",
		)
	}
	if !hasArrived {
		return CargoUnloadResult{}, fmt.Errorf(
			"cart has no completed delivery to the warehouse",
		)
	}

	// 4. 读取车上和仓库的铁矿库存。
	cartCargo := CargoStock{
		WorldID:    worldID,
		LocationID: CargoLocationCart,
		ItemID:     IronOreItemID,
	}

	warehouseCargo := CargoStock{
		WorldID:    worldID,
		LocationID: CargoLocationMerchant,
		ItemID:     IronOreItemID,
	}

	for _, stock := range []*CargoStock{&cartCargo, &warehouseCargo} {
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
			return CargoUnloadResult{}, fmt.Errorf(
				"load unloading stock at %q: %w",
				stock.LocationID,
				err,
			)
		}
	}

	if cartCargo.QuantityKg < quantityKg {
		return CargoUnloadResult{}, fmt.Errorf(
			"insufficient cart cargo: available %d kg, requested %d kg",
			cartCargo.QuantityKg,
			quantityKg,
		)
	}

	// 防止仓库库存相加超出 int64 范围。
	const maxQuantity int64 = 1<<63 - 1

	if warehouseCargo.QuantityKg > maxQuantity-quantityKg {
		return CargoUnloadResult{}, fmt.Errorf(
			"warehouse cargo quantity would overflow",
		)
	}

	// 5. 扣除车上库存。
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
		CargoLocationCart,
		IronOreItemID,
		cartCargo.Revision,
		quantityKg,
	)
	if err != nil {
		return CargoUnloadResult{}, fmt.Errorf(
			"deduct cart cargo: %w", err,
		)
	}

	affected, err = result.RowsAffected()
	if err != nil {
		return CargoUnloadResult{}, fmt.Errorf(
			"read cart cargo deduction result: %w", err,
		)
	}
	if affected != 1 {
		return CargoUnloadResult{}, fmt.Errorf(
			"cart cargo changed during unloading",
		)
	}

	// 6. 增加仓库库存。
	result, err = tx.ExecContext(ctx, `
		UPDATE cargo_stocks
		SET quantity_kg = quantity_kg + ?,
			revision = revision + 1
		WHERE world_id = ?
			AND location_id = ?
			AND item_id = ?
			AND revision = ?
			AND quantity_kg <= ?
	`,
		quantityKg,
		worldID,
		CargoLocationMerchant,
		IronOreItemID,
		warehouseCargo.Revision,
		maxQuantity-quantityKg,
	)
	if err != nil {
		return CargoUnloadResult{}, fmt.Errorf(
			"add warehouse cargo: %w", err,
		)
	}

	affected, err = result.RowsAffected()
	if err != nil {
		return CargoUnloadResult{}, fmt.Errorf(
			"read warehouse cargo update result: %w", err,
		)
	}
	if affected != 1 {
		return CargoUnloadResult{}, fmt.Errorf(
			"warehouse cargo changed during unloading",
		)
	}

	// 7. 载货量发生变化，更新货车版本。
	result, err = tx.ExecContext(ctx, `
		UPDATE carts
		SET revision = revision + 1
		WHERE world_id = ? AND id = ? AND revision = ?
	`, worldID, cart.ID, cart.Revision)
	if err != nil {
		return CargoUnloadResult{}, fmt.Errorf(
			"update unloaded cart revision: %w", err,
		)
	}

	affected, err = result.RowsAffected()
	if err != nil {
		return CargoUnloadResult{}, fmt.Errorf(
			"read unloaded cart update result: %w", err,
		)
	}
	if affected != 1 {
		return CargoUnloadResult{}, fmt.Errorf(
			"cart changed during unloading",
		)
	}

	// 8. 一起提交，避免只减车上库存、没有增加仓库库存。
	if err := tx.Commit(); err != nil {
		return CargoUnloadResult{}, fmt.Errorf(
			"commit cargo unloading: %w", err,
		)
	}

	cartCargo.QuantityKg -= quantityKg
	cartCargo.Revision++

	warehouseCargo.QuantityKg += quantityKg
	warehouseCargo.Revision++

	return CargoUnloadResult{
		UnloadedKg:     quantityKg,
		CartCargo:      cartCargo,
		WarehouseCargo: warehouseCargo,
		WorldRevision:  expectedRevision + 1,
	}, nil
}
