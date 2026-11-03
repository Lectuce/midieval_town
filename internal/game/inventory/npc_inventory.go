package game

import (
	"context"
	"database/sql"
	"fmt"
)

func GetNPCInventory(
	ctx context.Context,
	db *sql.DB,
	worldID string,
	npcID string,
) ([]NPCInventoryItem, error) {
	if worldID == "" || npcID == "" {
		return nil, fmt.Errorf(
			"world id and NPC id are required",
		)
	}

	// 区分“NPC 不存在”和“NPC 没有库存记录”。
	_, err := GetNPC(ctx, db, worldID, npcID)
	if err != nil {
		return nil, fmt.Errorf(
			"load inventory NPC: %w",
			err,
		)
	}

	rows, err := db.QueryContext(ctx, `
		SELECT item_id, quantity, revision
		FROM npc_inventory
		WHERE world_id = ? AND npc_id = ?
		ORDER BY item_id
	`, worldID, npcID)
	if err != nil {
		return nil, fmt.Errorf(
			"query NPC inventory: %w",
			err,
		)
	}
	defer rows.Close()

	items := make([]NPCInventoryItem, 0)

	for rows.Next() {
		var item NPCInventoryItem

		if err := rows.Scan(
			&item.ItemID,
			&item.Quantity,
			&item.Revision,
		); err != nil {
			return nil, fmt.Errorf(
				"scan NPC inventory item: %w",
				err,
			)
		}

		definition, err := GetItemDefinition(item.ItemID)
		if err != nil {
			return nil, fmt.Errorf(
				"load NPC inventory item definition: %w",
				err,
			)
		}

		item.Name = definition.Name
		items = append(items, item)
	}

	err = rows.Err()
	if err != nil {
		return nil, fmt.Errorf(
			"read NPC inventory: %w",
			err,
		)
	}

	return items, nil
}
