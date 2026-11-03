package game

import (
	"context"
	constError "cybertown/internal/game/const_error"
	"cybertown/internal/game/repair"
	"cybertown/internal/game/transport"
	worldstate "cybertown/internal/game/world"
	"database/sql"
	"fmt"
	"time"
)

func AdvanceWorldTime(
	ctx context.Context,
	db *sql.DB,
	worldID string,
	elapsed time.Duration,
	expectedRevision int64,
) (worldstate.World, error) {
	if worldID == "" {
		return worldstate.World{}, fmt.Errorf("world id is required")
	}

	if elapsed <= 0 {
		return worldstate.World{}, fmt.Errorf(
			"elapsed time must be positive",
		)
	}

	if expectedRevision < 1 {
		return worldstate.World{}, fmt.Errorf(
			"expected revision must be positive",
		)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return worldstate.World{}, fmt.Errorf(
			"begin world time advance: %w", err,
		)
	}
	defer tx.Rollback()

	// 1. 在事务中读取世界。
	var world worldstate.World
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
		return worldstate.World{}, fmt.Errorf(
			"load world for time advance: %w", err,
		)
	}

	world.GameTime, err = time.Parse(
		time.RFC3339Nano,
		rawGameTime,
	)
	if err != nil {
		return worldstate.World{}, fmt.Errorf(
			"parse world game time: %w", err,
		)
	}

	if world.Revision != expectedRevision {
		return worldstate.World{}, fmt.Errorf(
			"%w: expected %d, current %d",
			constError.ErrWorldRevisionConflict,
			expectedRevision,
			world.Revision,
		)
	}

	nextTime := world.GameTime.Add(elapsed).UTC()

	// 保证保存后的时间仍可按 RFC3339 格式读取。
	if nextTime.Year() < 0 || nextTime.Year() > 9999 {
		return worldstate.World{}, fmt.Errorf(
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
		return worldstate.World{}, fmt.Errorf(
			"advance world time: %w", err,
		)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return worldstate.World{}, fmt.Errorf(
			"read world time update result: %w", err,
		)
	}
	if affected != 1 {
		return worldstate.World{}, constError.ErrWorldRevisionConflict
	}

	// 3. 读取当前世界所有正在执行的维修。
	rows, err := tx.QueryContext(ctx, `
		SELECT id, cart_id, started_at, finish_at, revision
		FROM repair_jobs
		WHERE world_id = ? AND status = ?
		ORDER BY id
	`,
		worldID,
		repair.RepairJobStatusRunning,
	)
	if err != nil {
		return worldstate.World{}, fmt.Errorf(
			"query running repairs: %w", err,
		)
	}
	defer rows.Close()

	var dueJobs []repair.RepairJob

	for rows.Next() {
		var job repair.RepairJob
		var rawStartedAt, rawFinishAt string

		err := rows.Scan(
			&job.ID,
			&job.CartID,
			&rawStartedAt,
			&rawFinishAt,
			&job.Revision,
		)
		if err != nil {
			return worldstate.World{}, fmt.Errorf(
				"scan running repair: %w", err,
			)
		}

		job.StartedAt, err = time.Parse(
			time.RFC3339Nano,
			rawStartedAt,
		)
		if err != nil {
			return worldstate.World{}, fmt.Errorf(
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
			return worldstate.World{}, fmt.Errorf(
				"parse repair %q finish time: %w",
				job.ID,
				err,
			)
		}

		if !job.FinishAt.After(job.StartedAt) {
			return worldstate.World{}, fmt.Errorf(
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
		return worldstate.World{}, fmt.Errorf(
			"read running repairs: %w", err,
		)
	}

	// 先结束查询，再更新刚才查询的表。
	if err := rows.Close(); err != nil {
		return worldstate.World{}, fmt.Errorf(
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
			repair.RepairJobStatusCompleted,
			worldID,
			job.ID,
			repair.RepairJobStatusRunning,
			job.Revision,
		)
		if err != nil {
			return worldstate.World{}, fmt.Errorf(
				"complete repair %q: %w",
				job.ID,
				err,
			)
		}

		affected, err := result.RowsAffected()
		if err != nil {
			return worldstate.World{}, fmt.Errorf(
				"read repair completion result: %w", err,
			)
		}
		if affected != 1 {
			return worldstate.World{}, fmt.Errorf(
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
			transport.CartStatusReady,
			worldID,
			job.CartID,
			transport.CartStatusRepairing,
		)
		if err != nil {
			return worldstate.World{}, fmt.Errorf(
				"finish repairing cart %q: %w",
				job.CartID,
				err,
			)
		}

		affected, err = result.RowsAffected()
		if err != nil {
			return worldstate.World{}, fmt.Errorf(
				"read repaired cart update result: %w", err,
			)
		}
		if affected != 1 {
			return worldstate.World{}, fmt.Errorf(
				"cart %q is missing or is not repairing",
				job.CartID,
			)
		}
	}

	// 查询当前世界正在运输的任务。
	transportRows, err := tx.QueryContext(ctx, `
		SELECT
			id, cart_id,
			destination_scene_id,
			destination_x, destination_y, destination_floor,
			started_at, arrive_at, revision
		FROM transport_jobs
		WHERE world_id = ? AND status = ?
		ORDER BY id
	`,
		worldID,
		transport.TransportStatusInTransit,
	)
	if err != nil {
		return worldstate.World{}, fmt.Errorf(
			"query active transports: %w", err,
		)
	}
	defer transportRows.Close()

	var arrivedJobs []transport.TransportJob

	for transportRows.Next() {
		var job transport.TransportJob
		var rawStartedAt, rawArriveAt string

		err := transportRows.Scan(
			&job.ID,
			&job.CartID,
			&job.DestinationSceneID,
			&job.DestinationPosition.X,
			&job.DestinationPosition.Y,
			&job.DestinationFloor,
			&rawStartedAt,
			&rawArriveAt,
			&job.Revision,
		)
		if err != nil {
			return worldstate.World{}, fmt.Errorf(
				"scan active transport: %w", err,
			)
		}

		job.StartedAt, err = time.Parse(
			time.RFC3339Nano, rawStartedAt,
		)
		if err != nil {
			return worldstate.World{}, fmt.Errorf(
				"parse transport start time: %w", err,
			)
		}

		job.ArriveAt, err = time.Parse(
			time.RFC3339Nano, rawArriveAt,
		)
		if err != nil {
			return worldstate.World{}, fmt.Errorf(
				"parse transport arrival time: %w", err,
			)
		}

		if !job.ArriveAt.After(job.StartedAt) {
			return worldstate.World{}, fmt.Errorf(
				"transport %q has an invalid time range",
				job.ID,
			)
		}

		if !job.ArriveAt.After(nextTime) {
			arrivedJobs = append(arrivedJobs, job)
		}
	}

	if err := transportRows.Err(); err != nil {
		return worldstate.World{}, fmt.Errorf(
			"read active transports: %w", err,
		)
	}

	if err := transportRows.Close(); err != nil {
		return worldstate.World{}, fmt.Errorf(
			"close active transports query: %w", err,
		)
	}

	// 先关闭查询，再更新任务与货车。
	for _, job := range arrivedJobs {
		result, err := tx.ExecContext(ctx, `
		UPDATE transport_jobs
		SET status = ?,
			revision = revision + 1
		WHERE world_id = ?
			AND id = ?
			AND status = ?
			AND revision = ?
	`,
			transport.TransportStatusArrived,
			worldID,
			job.ID,
			transport.TransportStatusInTransit,
			job.Revision,
		)
		if err != nil {
			return worldstate.World{}, fmt.Errorf(
				"complete transport %q: %w",
				job.ID,
				err,
			)
		}

		affected, err := result.RowsAffected()
		if err != nil {
			return worldstate.World{}, fmt.Errorf(
				"read transport completion result: %w", err,
			)
		}
		if affected != 1 {
			return worldstate.World{}, fmt.Errorf(
				"transport %q changed during arrival",
				job.ID,
			)
		}

		result, err = tx.ExecContext(ctx, `
			UPDATE carts
			SET scene_id = ?,
				position_x = ?,
				position_y = ?,
				floor = ?,
				revision = revision + 1
			WHERE world_id = ?
				AND id = ?
				AND repair_status = ?
		`,
			job.DestinationSceneID,
			job.DestinationPosition.X,
			job.DestinationPosition.Y,
			job.DestinationFloor,
			worldID,
			job.CartID,
			transport.CartStatusReady,
		)
		if err != nil {
			return worldstate.World{}, fmt.Errorf(
				"move arriving cart: %w", err,
			)
		}

		affected, err = result.RowsAffected()
		if err != nil {
			return worldstate.World{}, fmt.Errorf(
				"read arriving cart update result: %w", err,
			)
		}
		if affected != 1 {
			return worldstate.World{}, fmt.Errorf(
				"arriving cart %q is missing or not ready",
				job.CartID,
			)
		}
	}

	// 使用本次推进后的游戏时间，更新 NPC 会面移动。
	// 与世界时间、维修和运输一起提交。
	err = advanceNegotiationMovementTx(
		ctx,
		tx,
		worldID,
		nextTime,
	)
	if err != nil {
		return worldstate.World{}, fmt.Errorf(
			"advance negotiation movement: %w",
			err,
		)
	}

	// 5. 时间、维修记录、货车状态一起提交。
	err = tx.Commit()
	if err != nil {
		return worldstate.World{}, fmt.Errorf(
			"commit world time advance: %w", err,
		)
	}

	world.GameTime = nextTime
	world.Revision++

	return world, nil
}
