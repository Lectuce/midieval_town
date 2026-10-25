package game

import (
	"context"
	"database/sql"
	"fmt"
)

func InitTransport(
	ctx context.Context,
	db *sql.DB,
	worldID string,
) error {
	if worldID == "" {
		return fmt.Errorf("world id is required")
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transport initialization: %w", err)
	}
	defer tx.Rollback()

	result, err := tx.ExecContext(ctx, `
		INSERT INTO carts (
			world_id, id, scene_id,
			position_x, position_y, floor,
			repair_status, capacity_kg, revision
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, 1)
		ON CONFLICT(world_id, id) DO NOTHING
	`,
		worldID,
		AbandonedCartID,
		"town",
		-5,
		-96,
		0,
		CartStatusDamaged,
		InitialCartCapacity,
	)
	if err != nil {
		return fmt.Errorf("initialize cart: %w", err)
	}

	inserted, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read cart initialization result: %w", err)
	}

	// 已有运输存档，不重置货车，也不重新发放铁矿。
	if inserted == 0 {
		return nil
	}

	stocks := []CargoStock{
		{
			LocationID: CargoLocationCartSite,
			ItemID:     IronOreItemID,
			QuantityKg: InitialIronOreKg,
		},
		{
			LocationID: CargoLocationCart,
			ItemID:     IronOreItemID,
			QuantityKg: 0,
		},
		{
			LocationID: CargoLocationMerchant,
			ItemID:     IronOreItemID,
			QuantityKg: 0,
		},
	}

	for _, stock := range stocks {
		_, err := tx.ExecContext(ctx, `
			INSERT INTO cargo_stocks (
				world_id, location_id, item_id,
				quantity_kg, revision
			)
			VALUES (?, ?, ?, ?, 1)
		`,
			worldID,
			stock.LocationID,
			stock.ItemID,
			stock.QuantityKg,
		)
		if err != nil {
			return fmt.Errorf(
				"initialize cargo stock at %q: %w",
				stock.LocationID,
				err,
			)
		}
	}

	// 为已有世界新增了运输状态，世界版本同步变化。
	_, err = tx.ExecContext(ctx, `
		UPDATE worlds
		SET revision = revision + 1
		WHERE id = ?
	`, worldID)
	if err != nil {
		return fmt.Errorf("update world revision: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transport initialization: %w", err)
	}

	return nil
}
