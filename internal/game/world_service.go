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

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return World{}, fmt.Errorf(
			"begin world time advance: %w", err,
		)
	}
	defer tx.Rollback()

	// 1. 在事务中读取世界。
	var world World
	var rawGameTime string

	err = tx.QueryRowContext(ctx, `
		SELECT id, game_time, revision
		FROM worlds
		WHERE id = ?
	`, worldID).Scan(
		&world.ID,
		&rawGameTime,
		&world.Revision,
	)
	if err != nil {
		return World{}, fmt.Errorf(
			"load world for time advance: %w", err,
		)
	}

	world.GameTime, err = time.Parse(
		time.RFC3339Nano,
		rawGameTime,
	)
	if err != nil {
		return World{}, fmt.Errorf(
			"parse world game time: %w", err,
		)
	}

	if world.Revision != expectedRevision {
		return World{}, fmt.Errorf(
			"%w: expected %d, current %d",
			ErrWorldRevisionConflict,
			expectedRevision,
			world.Revision,
		)
	}

	nextTime := world.GameTime.Add(elapsed).UTC()

	// 保证保存后的时间仍可按 RFC3339 格式读取。
	if nextTime.Year() < 0 || nextTime.Year() > 9999 {
		return World{}, fmt.Errorf(
			"next game time is outside the supported range",
		)
	}

	// 2. 更新世界时间，但此时还没有提交。
	// 后续结算失败，这次时间更新也会回滚。
	result, err := tx.ExecContext(ctx, `
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
			"advance world time: %w", err,
		)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return World{}, fmt.Errorf(
			"read world time update result: %w", err,
		)
	}
	if affected != 1 {
		return World{}, ErrWorldRevisionConflict
	}

	// 3. 读取当前世界所有正在执行的维修。
	rows, err := tx.QueryContext(ctx, `
		SELECT id, cart_id, started_at, finish_at, revision
		FROM repair_jobs
		WHERE world_id = ? AND status = ?
		ORDER BY id
	`,
		worldID,
		RepairJobStatusRunning,
	)
	if err != nil {
		return World{}, fmt.Errorf(
			"query running repairs: %w", err,
		)
	}
	defer rows.Close()

	var dueJobs []RepairJob

	for rows.Next() {
		var job RepairJob
		var rawStartedAt, rawFinishAt string

		err := rows.Scan(
			&job.ID,
			&job.CartID,
			&rawStartedAt,
			&rawFinishAt,
			&job.Revision,
		)
		if err != nil {
			return World{}, fmt.Errorf(
				"scan running repair: %w", err,
			)
		}

		job.StartedAt, err = time.Parse(
			time.RFC3339Nano,
			rawStartedAt,
		)
		if err != nil {
			return World{}, fmt.Errorf(
				"parse repair %q start time: %w",
				job.ID,
				err,
			)
		}

		job.FinishAt, err = time.Parse(
			time.RFC3339Nano,
			rawFinishAt,
		)
		if err != nil {
			return World{}, fmt.Errorf(
				"parse repair %q finish time: %w",
				job.ID,
				err,
			)
		}

		if !job.FinishAt.After(job.StartedAt) {
			return World{}, fmt.Errorf(
				"repair %q has an invalid time range",
				job.ID,
			)
		}

		// 完成时间 <= 推进后的游戏时间，才需要结算。
		if !job.FinishAt.After(nextTime) {
			dueJobs = append(dueJobs, job)
		}
	}

	if err := rows.Err(); err != nil {
		return World{}, fmt.Errorf(
			"read running repairs: %w", err,
		)
	}

	// 先结束查询，再更新刚才查询的表。
	if err := rows.Close(); err != nil {
		return World{}, fmt.Errorf(
			"close running repairs query: %w", err,
		)
	}

	// 4. 结算已经到期的维修。
	for _, job := range dueJobs {
		result, err := tx.ExecContext(ctx, `
			UPDATE repair_jobs
			SET status = ?,
				revision = revision + 1
			WHERE world_id = ?
				AND id = ?
				AND status = ?
				AND revision = ?
		`,
			RepairJobStatusCompleted,
			worldID,
			job.ID,
			RepairJobStatusRunning,
			job.Revision,
		)
		if err != nil {
			return World{}, fmt.Errorf(
				"complete repair %q: %w",
				job.ID,
				err,
			)
		}

		affected, err := result.RowsAffected()
		if err != nil {
			return World{}, fmt.Errorf(
				"read repair completion result: %w", err,
			)
		}
		if affected != 1 {
			return World{}, fmt.Errorf(
				"repair %q changed during settlement",
				job.ID,
			)
		}

		result, err = tx.ExecContext(ctx, `
			UPDATE carts
			SET repair_status = ?,
				revision = revision + 1
			WHERE world_id = ?
				AND id = ?
				AND repair_status = ?
		`,
			CartStatusReady,
			worldID,
			job.CartID,
			CartStatusRepairing,
		)
		if err != nil {
			return World{}, fmt.Errorf(
				"finish repairing cart %q: %w",
				job.CartID,
				err,
			)
		}

		affected, err = result.RowsAffected()
		if err != nil {
			return World{}, fmt.Errorf(
				"read repaired cart update result: %w", err,
			)
		}
		if affected != 1 {
			return World{}, fmt.Errorf(
				"cart %q is missing or is not repairing",
				job.CartID,
			)
		}
	}

	// 5. 时间、维修记录、货车状态一起提交。
	if err := tx.Commit(); err != nil {
		return World{}, fmt.Errorf(
			"commit world time advance: %w", err,
		)
	}

	world.GameTime = nextTime
	world.Revision++

	return world, nil
}
