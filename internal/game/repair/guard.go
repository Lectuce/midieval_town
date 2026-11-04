package repair

import (
	"context"
	"database/sql"
	"errors"

	"cybertown/internal/game/storage"
)

var ErrRepairInNegotiation = errors.New("货车正在会面协商，请在会面中处理报价")

// ownerID 为空：普通聊天、普通维修入口。
// ownerID 非空：由该协商的 GameService 在事务内调用。
func CheckRepairNegotiation(
	ctx context.Context,
	q storage.DBTX,
	worldID string,
	cartID string,
	ownerID string,
) error {
	var activeID string

	err := q.QueryRowContext(ctx, `
		SELECT id
		FROM negotiations
		WHERE world_id = ?
			AND cart_id = ?
			AND status IN ('inviting', 'travelling', 'discussing')
	`, worldID, cartID).Scan(&activeID)

	if errors.Is(err, sql.ErrNoRows) {
		if ownerID != "" {
			return ErrRepairInNegotiation
		}
		return nil
	}
	if err != nil {
		return err
	}

	if ownerID == "" || activeID != ownerID {
		return ErrRepairInNegotiation
	}

	var discussing bool
	err = q.QueryRowContext(ctx, `
		SELECT status = 'discussing'
		FROM negotiations
		WHERE world_id = ? AND id = ?
	`, worldID, ownerID).Scan(&discussing)
	if err != nil {
		return err
	}
	if !discussing {
		return ErrRepairInNegotiation
	}

	return nil
}
