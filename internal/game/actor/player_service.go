package game

import (
	"context"
	"errors"
	"fmt"
	"math"
)

var ErrPlayerRevisionConflict = errors.New("player revision conflict")

func (s *Service) GetPlayer(
	ctx context.Context,
	worldID string,
	playerID string,
) (Player, error) {
	return getPlayer(ctx, s.db, worldID, playerID)
}

func (s *Service) SavePlayerPosition(ctx context.Context, worldID string, playerID string, target Position, floor int, expectedRevision int64) (Player, error) {

	if worldID == "" || playerID == "" {
		return Player{}, fmt.Errorf("world id and player id are required")
	}

	if expectedRevision < 1 {
		return Player{}, fmt.Errorf("expected revision must be positive")
	}

	if floor < 0 || floor > 3 {
		return Player{}, fmt.Errorf("invalid floor: %d", floor)
	}

	if math.IsNaN(target.X) || math.IsNaN(target.Y) || math.IsInf(target.X, 0) || math.IsInf(target.Y, 0) {
		return Player{}, fmt.Errorf("target position must be finite")
	}

	player, err := getPlayer(ctx, s.db, worldID, playerID)
	if err != nil {
		return Player{}, err
	}

	if player.Status != "active" {
		return Player{}, ErrPlayerInactive
	}

	if player.Revision != expectedRevision {
		return Player{}, fmt.Errorf(
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
		return Player{}, fmt.Errorf("move player: %w", err)
	}

	player.Position = target
	player.Floor = floor
	player.Revision++

	return player, nil
}
