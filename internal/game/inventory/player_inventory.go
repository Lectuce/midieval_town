package game

import (
	"context"
	"database/sql"
	"fmt"
)

func GetInventory(
	ctx context.Context,
	db *sql.DB,
	worldID string,
	playerID string,
) ([]InventoryItem, error) {

	if worldID == "" || playerID == "" {
		return nil, fmt.Errorf(
			"world_id and player_id are required",
		)
	}

	_, err := GetPlayer(ctx, db, worldID, playerID)
	if err != nil {
		return nil, fmt.Errorf("load inventory player: %w", err)
	}

	items := make([]InventoryItem, 0)
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("get inventory: %w", err)
	}
	defer tx.Rollback()

	rows, err := tx.QueryContext(ctx, `
		SELECT item_id, quantity
		FROM player_inventory
		WHERE world_id = ? AND player_id = ? AND quantity > 0
		ORDER BY item_id;
		`,
		worldID,
		playerID,
	)

	if err != nil {
		return nil, fmt.Errorf("query inventory: %w", err)
	}

	defer rows.Close()

	for rows.Next() {
		var item InventoryItem

		err := rows.Scan(&item.ItemID, &item.Quantity)
		if err != nil {
			return nil, fmt.Errorf("scan inventory item: %w", err)
		}

		definition, err := GetItemDefinition(item.ItemID)
		if err != nil {
			return nil, fmt.Errorf(
				"load inventory item definition: %w",
				err,
			)
		}

		item.Name = definition.Name
		items = append(items, item)

	}
	err = rows.Err()
	if err != nil {
		return nil, fmt.Errorf("read inventory: %w", err)
	}

	return items, nil

}
