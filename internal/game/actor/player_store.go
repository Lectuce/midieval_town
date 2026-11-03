package actor

import (
	"context"
	"cybertown/internal/game/model"
	"database/sql"
	"fmt"
)

func GetPlayer(
	ctx context.Context,
	db *sql.DB,
	worldID string,
	playerID string,
) (model.Player, error) {
	return getPlayer(ctx, db, worldID, playerID)
}

func getPlayer(ctx context.Context, db dbtx, worldID string, playerID string) (Player, error) {

	var player model.Player

	err := dbtx.QueryRowContext(ctx,
		`
		SELECT
			world_id, id, name, scene_id, status,
			position_x, position_y, floor, revision
		FROM players
		WHERE world_id = ? AND id = ?
	`, worldID, playerID).Scan(
		&player.WorldID,
		&player.ID,
		&player.Name,
		&player.SceneID,
		&player.Status,
		&player.Position.X,
		&player.Position.Y,
		&player.Floor,
		&player.Revision,
	)

	if err != nil {
		return model.Player{}, fmt.Errorf(
			"get player %q in world %q: %w",
			playerID,
			worldID,
			err,
		)
	}

	return player, nil

}

// 更新玩家位置
func updatePlayerPosition(ctx context.Context,
	db dbtx,
	worldID string,
	playerID string,
	position Position,
	floor int,
	expectedRevision int64,
) error {
	if floor < 0 || floor > 3 {
		return fmt.Errorf("invalid floor: %d", floor)
	}

	if expectedRevision < 1 {
		return fmt.Errorf("expected revision must be positive")
	}

	result, err := db.ExecContext(ctx, `
		UPDATE players
		SET position_x = ?,
			position_y = ?,
			floor = ?,
			revision = revision + 1
		WHERE world_id = ?
			AND id = ?
			AND revision = ?
	`,
		position.X,
		position.Y,
		floor,
		worldID,
		playerID,
		expectedRevision,
	)
	if err != nil {
		return fmt.Errorf("update player position: %w", err)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read position update result: %w", err)
	}

	if affected == 0 {
		return ErrPlayerRevisionConflict
	}

	return nil
}
