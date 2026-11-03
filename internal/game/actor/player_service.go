package actor

import (
	"context"
	"cybertown/internal/game/model"
	"fmt"
	"math"
)

func (s *Service) GetPlayer(
	ctx context.Context,
	worldID string,
	playerID string,
) (model.Player, error) {
	return getPlayer(ctx, s.db, worldID, playerID)
}

func (s *Service) SavePlayerPosition(ctx context.Context, worldID string, playerID string, target Position, floor int, expectedRevision int64) (Player, error) {

	if worldID == "" || playerID == "" {
		return model.Player{}, fmt.Errorf("world id and player id are required")
	}

	if expectedRevision < 1 {
		return model.Player{}, fmt.Errorf("expected revision must be positive")
	}

	if floor < 0 || floor > 3 {
		return model.Player{}, fmt.Errorf("invalid floor: %d", floor)
	}

	if math.IsNaN(target.X) || math.IsNaN(target.Y) || math.IsInf(target.X, 0) || math.IsInf(target.Y, 0) {
		return model.Player{}, fmt.Errorf("target position must be finite")
	}

	player, err := getPlayer(ctx, s.db, worldID, playerID)
	if err != nil {
		return model.Player{}, err
	}

	if player.Status != "active" {
		return model.Player{}, ErrPlayerInactive
	}

	if player.Revision != expectedRevision {
		return model.Player{}, fmt.Errorf(
			"%w: expected %d, current %d",
			ErrPlayerRevisionConflict,
			expectedRevision,
			player.Revision,
		)
	}

	// 坐标和楼层都没有变化，就不用更新存档。
	if player.Position == target && player.Floor == floor {
		return player, nil
	}

	err = updatePlayerPosition(ctx, s.db, player.WorldID, playerID, target, floor, expectedRevision)
	if err != nil {
		return model.Player{}, fmt.Errorf("move player: %w", err)
	}

	player.Position = target
	player.Floor = floor
	player.Revision++

	return player, nil
}
