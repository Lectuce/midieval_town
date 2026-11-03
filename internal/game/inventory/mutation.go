package inventory

import (
	"context"
	constError "cybertown/internal/game/const_error"
	"database/sql"
	"fmt"
)

const maxQuantity int64 = 1<<63 - 1

func validateMutation(
	tx *sql.Tx,
	worldID string,
	ownerID string,
	itemID string,
	quantity int64,
) error {
	if tx == nil {
		return fmt.Errorf("transaction is required")
	}
	if worldID == "" || ownerID == "" || itemID == "" {
		return fmt.Errorf("world, owner and item are required")
	}
	if quantity <= 0 {
		return fmt.Errorf("quantity must be positive")
	}
	if _, err := GetItemDefinition(itemID); err != nil {
		return err
	}
	return nil
}

func requireOneRow(result sql.Result, mismatch error) error {
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read inventory update result: %w", err)
	}
	if count != 1 {
		return mismatch
	}
	return nil
}

// AddPlayerItemTx 增加玩家物品。
// 不开启或提交事务，由调用方负责事务生命周期。
func AddPlayerItemTx(
	ctx context.Context,
	tx *sql.Tx,
	worldID string,
	playerID string,
	itemID string,
	quantity int64,
) error {
	if err := validateMutation(
		tx, worldID, playerID, itemID, quantity,
	); err != nil {
		return err
	}

	result, err := tx.ExecContext(ctx, `
		INSERT INTO player_inventory (
			world_id, player_id, item_id, quantity
		)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(world_id, player_id, item_id)
		DO UPDATE SET
			quantity = player_inventory.quantity + excluded.quantity
		WHERE player_inventory.quantity <= ?
	`,
		worldID,
		playerID,
		itemID,
		quantity,
		maxQuantity-quantity,
	)
	if err != nil {
		return fmt.Errorf("add player item: %w", err)
	}

	return requireOneRow(result, constError.ErrQuantityOverflow)
}

// ConsumePlayerItemTx 仅在库存足够时扣除玩家物品。
func ConsumePlayerItemTx(
	ctx context.Context,
	tx *sql.Tx,
	worldID string,
	playerID string,
	itemID string,
	quantity int64,
) error {
	if err := validateMutation(
		tx, worldID, playerID, itemID, quantity,
	); err != nil {
		return err
	}

	result, err := tx.ExecContext(ctx, `
		UPDATE player_inventory
		SET quantity = quantity - ?
		WHERE world_id = ?
			AND player_id = ?
			AND item_id = ?
			AND quantity >= ?
	`,
		quantity,
		worldID,
		playerID,
		itemID,
		quantity,
	)
	if err != nil {
		return fmt.Errorf("consume player item: %w", err)
	}

	return requireOneRow(result, constError.ErrInsufficientQuantity)
}

// ConsumeNPCItemTx 扣除 NPC 物品，同时增加库存版本。
func ConsumeNPCItemTx(
	ctx context.Context,
	tx *sql.Tx,
	worldID string,
	npcID string,
	itemID string,
	quantity int64,
) error {
	if err := validateMutation(
		tx, worldID, npcID, itemID, quantity,
	); err != nil {
		return err
	}

	result, err := tx.ExecContext(ctx, `
		UPDATE npc_inventory
		SET quantity = quantity - ?,
			revision = revision + 1
		WHERE world_id = ?
			AND npc_id = ?
			AND item_id = ?
			AND quantity >= ?
	`,
		quantity,
		worldID,
		npcID,
		itemID,
		quantity,
	)
	if err != nil {
		return fmt.Errorf("consume NPC item: %w", err)
	}

	return requireOneRow(result, constError.ErrInsufficientQuantity)
}

// TransferNPCItemTx 在两个 NPC 之间转移物品。
// 延续当前维修规则：接收方必须已有对应库存行。
// 任一步失败，调用方必须回滚整个事务。
func TransferNPCItemTx(
	ctx context.Context,
	tx *sql.Tx,
	worldID string,
	fromNPCID string,
	toNPCID string,
	itemID string,
	quantity int64,
) error {
	if toNPCID == "" || fromNPCID == toNPCID {
		return fmt.Errorf("invalid inventory transfer participants")
	}

	if err := ConsumeNPCItemTx(
		ctx, tx, worldID, fromNPCID, itemID, quantity,
	); err != nil {
		return err
	}

	result, err := tx.ExecContext(ctx, `
		UPDATE npc_inventory
		SET quantity = quantity + ?,
			revision = revision + 1
		WHERE world_id = ?
			AND npc_id = ?
			AND item_id = ?
			AND quantity <= ?
	`,
		quantity,
		worldID,
		toNPCID,
		itemID,
		maxQuantity-quantity,
	)
	if err != nil {
		return fmt.Errorf("credit NPC item: %w", err)
	}

	return requireOneRow(result, constError.ErrCreditRejected)
}

// GetPlayerQuantityTx 返回当前事务中的玩家物品数量。
// 没有库存行时返回零。
func GetPlayerQuantityTx(
	ctx context.Context,
	tx *sql.Tx,
	worldID string,
	playerID string,
	itemID string,
) (int64, error) {
	if tx == nil {
		return 0, fmt.Errorf("transaction is required")
	}

	var quantity int64
	err := tx.QueryRowContext(ctx, `
		SELECT COALESCE((
			SELECT quantity
			FROM player_inventory
			WHERE world_id = ?
				AND player_id = ?
				AND item_id = ?
		), 0)
	`, worldID, playerID, itemID).Scan(&quantity)
	if err != nil {
		return 0, fmt.Errorf("get player quantity: %w", err)
	}

	return quantity, nil
}
