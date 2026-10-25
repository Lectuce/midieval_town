package game

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

var ErrWorldRevisionConflict = errors.New(
	"world revision conflict",
)

func AdvanceWorldTime(
	ctx context.Context,
	db *sql.DB,
	worldID string,
	elapsed time.Duration,
	expectedRevision int64,
) (World, error) {
	if worldID == "" {
		return World{}, fmt.Errorf("world id is required")
	}

	if elapsed <= 0 {
		return World{}, fmt.Errorf(
			"elapsed time must be positive",
		)
	}

	if expectedRevision < 1 {
		return World{}, fmt.Errorf(
			"expected revision must be positive",
		)
	}

	world, err := GetWorld(ctx, db, worldID)
	if err != nil {
		return World{}, err
	}

	if world.Revision != expectedRevision {
		return World{}, fmt.Errorf(
			"%w: expected %d, current %d",
			ErrWorldRevisionConflict,
			expectedRevision,
			world.Revision,
		)
	}

	nextTime := world.GameTime.Add(elapsed)

	result, err := db.ExecContext(ctx, `
		UPDATE worlds
		SET game_time = ?,
			revision = revision + 1
		WHERE id = ?
			AND revision = ?
	`,
		nextTime.Format(time.RFC3339Nano),
		worldID,
		expectedRevision,
	)
	if err != nil {
		return World{}, fmt.Errorf(
			"advance world time: %w",
			err,
		)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return World{}, fmt.Errorf(
			"read world time update result: %w",
			err,
		)
	}

	if affected == 0 {
		return World{}, ErrWorldRevisionConflict
	}

	world.GameTime = nextTime
	world.Revision++

	return world, nil
}
