package game

import (
	"context"
	"database/sql"
	"fmt"
)

func InitNPCInventory(
	ctx context.Context,
	db *sql.DB,
	worldID string,
) error {
	if worldID == "" {
		return fmt.Errorf("world id is required")
	}

	// 初始资源，不代表报价或付款授权。
	initialItems := []struct {
		NPCID    string
		ItemID   string
		Quantity int64
	}{
		{
			NPCID:    "merchant",
			ItemID:   "coin",
			Quantity: 20,
		},
		{
			NPCID:    "blacksmith",
			ItemID:   "coin",
			Quantity: 0,
		},
		{
			NPCID:    "blacksmith",
			ItemID:   "repair_material",
			Quantity: 3,
		},
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf(
			"begin NPC inventory initialization: %w",
			err,
		)
	}
	defer tx.Rollback()

	var insertedCount int64

	for _, item := range initialItems {
		if _, err := GetItemDefinition(item.ItemID); err != nil {
			return fmt.Errorf(
				"validate initial NPC item: %w",
				err,
			)
		}

		result, err := tx.ExecContext(ctx, `
			INSERT INTO npc_inventory (
				world_id, npc_id, item_id,
				quantity, revision
			)
			VALUES (?, ?, ?, ?, 1)
			ON CONFLICT(world_id, npc_id, item_id) DO NOTHING
		`,
			worldID,
			item.NPCID,
			item.ItemID,
			item.Quantity,
		)
		if err != nil {
			return fmt.Errorf(
				"initialize NPC %q item %q: %w",
				item.NPCID,
				item.ItemID,
				err,
			)
		}

		inserted, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf(
				"read NPC inventory initialization result: %w",
				err,
			)
		}

		insertedCount += inserted
	}

	// 只有实际新增资源记录时，才更新世界版本。
	if insertedCount > 0 {
		_, err = tx.ExecContext(ctx, `
			UPDATE worlds
			SET revision = revision + 1
			WHERE id = ?
		`, worldID)
		if err != nil {
			return fmt.Errorf(
				"update world inventory revision: %w",
				err,
			)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf(
			"commit NPC inventory initialization: %w",
			err,
		)
	}

	return nil
}
