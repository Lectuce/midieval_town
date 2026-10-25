package game

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

func GetWorld(
	ctx context.Context,
	db *sql.DB,
	worldID string,
) (World, error) {
	if worldID == "" {
		return World{}, fmt.Errorf("world id is required")
	}

	var world World
	var gameTime string

	err := db.QueryRowContext(ctx, `
		SELECT id, game_time, revision
		FROM worlds
		WHERE id = ?
	`, worldID).Scan(
		&world.ID,
		&gameTime,
		&world.Revision,
	)
	if err != nil {
		return World{}, fmt.Errorf(
			"get world %q: %w",
			worldID,
			err,
		)
	}

	world.GameTime, err = time.Parse(
		time.RFC3339Nano,
		gameTime,
	)
	if err != nil {
		return World{}, fmt.Errorf(
			"parse world %q game time: %w",
			worldID,
			err,
		)
	}

	return world, nil
}
