package game

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

func GetActiveRepairJob(
	ctx context.Context,
	db *sql.DB,
	worldID string,
	cartID string,
) (RepairJob, error) {
	if worldID == "" || cartID == "" {
		return RepairJob{}, fmt.Errorf(
			"world id and cart id are required",
		)
	}

	var job RepairJob
	var startedAt string
	var finishAt string

	err := db.QueryRowContext(ctx, `
		SELECT
			world_id, id, cart_id, worker_npc_id,
			status, started_at, finish_at, revision
		FROM repair_jobs
		WHERE world_id = ?
			AND cart_id = ?
			AND status = ?
	`,
		worldID,
		cartID,
		RepairJobStatusRunning,
	).Scan(
		&job.WorldID,
		&job.ID,
		&job.CartID,
		&job.WorkerNPCID,
		&job.Status,
		&startedAt,
		&finishAt,
		&job.Revision,
	)
	if err != nil {
		return RepairJob{}, fmt.Errorf(
			"get active repair for cart %q: %w",
			cartID,
			err,
		)
	}

	job.StartedAt, err = time.Parse(
		time.RFC3339Nano,
		startedAt,
	)
	if err != nil {
		return RepairJob{}, fmt.Errorf(
			"parse repair %q start time: %w",
			job.ID,
			err,
		)
	}

	job.FinishAt, err = time.Parse(
		time.RFC3339Nano,
		finishAt,
	)
	if err != nil {
		return RepairJob{}, fmt.Errorf(
			"parse repair %q finish time: %w",
			job.ID,
			err,
		)
	}

	if !job.FinishAt.After(job.StartedAt) {
		return RepairJob{}, fmt.Errorf(
			"repair %q finish time must be after start time",
			job.ID,
		)
	}

	return job, nil
}
