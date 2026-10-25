package game

import (
	"context"
	"database/sql"
	"fmt"
)

func GetCart(
	ctx context.Context,
	db *sql.DB,
	worldID string,
	cartID string,
) (Cart, error) {
	if worldID == "" || cartID == "" {
		return Cart{}, fmt.Errorf(
			"world id and cart id are required",
		)
	}

	var cart Cart

	err := db.QueryRowContext(ctx, `
		SELECT
			world_id, id, scene_id,
			position_x, position_y, floor,
			repair_status, capacity_kg, revision
		FROM carts
		WHERE world_id = ? AND id = ?
	`, worldID, cartID).Scan(
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
		return Cart{}, fmt.Errorf(
			"get cart %q: %w",
			cartID,
			err,
		)
	}

	return cart, nil
}

func GetCargoStock(
	ctx context.Context,
	db *sql.DB,
	worldID string,
	locationID string,
	itemID string,
) (CargoStock, error) {
	if worldID == "" || locationID == "" || itemID == "" {
		return CargoStock{}, fmt.Errorf(
			"world id, location id and item id are required",
		)
	}

	var stock CargoStock

	err := db.QueryRowContext(ctx, `
		SELECT
			world_id, location_id, item_id,
			quantity_kg, revision
		FROM cargo_stocks
		WHERE world_id = ?
			AND location_id = ?
			AND item_id = ?
	`, worldID, locationID, itemID).Scan(
		&stock.WorldID,
		&stock.LocationID,
		&stock.ItemID,
		&stock.QuantityKg,
		&stock.Revision,
	)
	if err != nil {
		return CargoStock{}, fmt.Errorf(
			"get cargo stock at %q: %w",
			locationID,
			err,
		)
	}

	return stock, nil
}
