package game

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

func StartCartRepair(
	ctx context.Context,
	db *sql.DB,
	worldID string,
	proposalID string,
	expectedRevision int64,
) (RepairJob, error) {
	if worldID == "" || proposalID == "" {
		return RepairJob{}, fmt.Errorf(
			"world id and proposal id are required",
		)
	}
	if expectedRevision < 1 {
		return RepairJob{}, fmt.Errorf(
			"expected revision must be positive",
		)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return RepairJob{}, fmt.Errorf(
			"begin cart repair: %w", err,
		)
	}
	defer tx.Rollback()

	// 1. 在事务中读取提议。
	var proposal RepairProposal

	err = tx.QueryRowContext(ctx, `
		SELECT
			world_id, id, cart_id,
			payer_npc_id, worker_npc_id,
			price_coins, material_count, duration_minutes,
			payer_confirmed, worker_confirmed,
			status, COALESCE(repair_job_id, ''), revision
		FROM repair_proposals
		WHERE world_id = ? AND id = ?
	`, worldID, proposalID).Scan(
		&proposal.WorldID,
		&proposal.ID,
		&proposal.CartID,
		&proposal.PayerNPCID,
		&proposal.WorkerNPCID,
		&proposal.PriceCoins,
		&proposal.MaterialCount,
		&proposal.DurationMinutes,
		&proposal.PayerConfirmed,
		&proposal.WorkerConfirmed,
		&proposal.Status,
		&proposal.RepairJobID,
		&proposal.Revision,
	)
	if err != nil {
		return RepairJob{}, fmt.Errorf(
			"load repair proposal: %w", err,
		)
	}

	// 2. 已执行的提议直接返回原维修记录。
	// 放在版本检查之前：第一次成功后提议版本会变化，
	// 重试仍应返回原结果，而不是再次扣款。
	if proposal.Status == RepairProposalStatusExecuted {
		var job RepairJob
		var startedAt, finishAt string

		err = tx.QueryRowContext(ctx, `
			SELECT
				world_id, id, cart_id, worker_npc_id,
				status, started_at, finish_at, revision
			FROM repair_jobs
			WHERE world_id = ? AND id = ?
		`, worldID, proposal.RepairJobID).Scan(
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
				"load existing repair job: %w", err,
			)
		}

		job.StartedAt, err = time.Parse(
			time.RFC3339Nano, startedAt,
		)
		if err != nil {
			return RepairJob{}, fmt.Errorf(
				"parse existing repair start time: %w", err,
			)
		}

		job.FinishAt, err = time.Parse(
			time.RFC3339Nano, finishAt,
		)
		if err != nil {
			return RepairJob{}, fmt.Errorf(
				"parse existing repair finish time: %w", err,
			)
		}

		return job, nil
	}

	// 3. 检查版本、确认状态和条款。
	if proposal.Revision != expectedRevision {
		return RepairJob{}, fmt.Errorf(
			"%w: expected %d, current %d",
			ErrRepairProposalRevisionConflict,
			expectedRevision,
			proposal.Revision,
		)
	}

	if proposal.Status != RepairProposalStatusConfirmed ||
		!proposal.PayerConfirmed ||
		!proposal.WorkerConfirmed {
		return RepairJob{}, fmt.Errorf(
			"repair proposal is not confirmed by both participants",
		)
	}

	if proposal.PriceCoins < 0 ||
		proposal.MaterialCount <= 0 ||
		proposal.DurationMinutes < 1 ||
		proposal.DurationMinutes > 1440 ||
		proposal.PayerNPCID == proposal.WorkerNPCID {
		return RepairJob{}, fmt.Errorf(
			"invalid repair proposal terms",
		)
	}

	// 4. 当前规则：只有铁匠可以执行维修。
	var workerRole string

	err = tx.QueryRowContext(ctx, `
		SELECT role
		FROM npcs
		WHERE world_id = ? AND id = ?
	`, worldID, proposal.WorkerNPCID).Scan(&workerRole)
	if err != nil {
		return RepairJob{}, fmt.Errorf(
			"load repair worker: %w", err,
		)
	}

	if workerRole != "blacksmith" {
		return RepairJob{}, fmt.Errorf(
			"NPC %q cannot repair carts",
			proposal.WorkerNPCID,
		)
	}

	// 检查货车或维修者是否已有正在执行的维修。
	var busy bool

	err = tx.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM repair_jobs
			WHERE world_id = ?
				AND status = ?
				AND (cart_id = ? OR worker_npc_id = ?)
		)
	`,
		worldID,
		RepairJobStatusRunning,
		proposal.CartID,
		proposal.WorkerNPCID,
	).Scan(&busy)
	if err != nil {
		return RepairJob{}, fmt.Errorf(
			"check repair availability: %w", err,
		)
	}

	if busy {
		return RepairJob{}, fmt.Errorf(
			"cart or worker already has a running repair",
		)
	}

	// 5. 使用世界的游戏时间，不使用现实时间 time.Now()。
	var rawGameTime string

	err = tx.QueryRowContext(ctx, `
		SELECT game_time
		FROM worlds
		WHERE id = ?
	`, worldID).Scan(&rawGameTime)
	if err != nil {
		return RepairJob{}, fmt.Errorf(
			"load repair game time: %w", err,
		)
	}

	startedAt, err := time.Parse(
		time.RFC3339Nano, rawGameTime,
	)
	if err != nil {
		return RepairJob{}, fmt.Errorf(
			"parse repair game time: %w", err,
		)
	}

	startedAt = startedAt.UTC()
	finishAt := startedAt.Add(
		time.Duration(proposal.DurationMinutes) * time.Minute,
	)

	job := RepairJob{
		WorldID:     worldID,
		ID:          "repair-" + proposal.ID,
		CartID:      proposal.CartID,
		WorkerNPCID: proposal.WorkerNPCID,
		Status:      RepairJobStatusRunning,
		StartedAt:   startedAt,
		FinishAt:    finishAt,
		Revision:    1,
	}

	// 6. 货车必须仍然损坏。
	// 此修改还未提交；后续任何失败都会一起回滚。
	result, err := tx.ExecContext(ctx, `
		UPDATE carts
		SET repair_status = ?,
			revision = revision + 1
		WHERE world_id = ?
			AND id = ?
			AND repair_status = ?
	`,
		CartStatusRepairing,
		worldID,
		proposal.CartID,
		CartStatusDamaged,
	)
	if err != nil {
		return RepairJob{}, fmt.Errorf(
			"set cart repairing: %w", err,
		)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return RepairJob{}, fmt.Errorf(
			"read cart repair update result: %w", err,
		)
	}
	if affected != 1 {
		return RepairJob{}, fmt.Errorf(
			"cart is missing or is not damaged",
		)
	}

	// 7. 消耗维修者提供的材料。
	result, err = tx.ExecContext(ctx, `
		UPDATE npc_inventory
		SET quantity = quantity - ?,
			revision = revision + 1
		WHERE world_id = ?
			AND npc_id = ?
			AND item_id = 'repair_material'
			AND quantity >= ?
	`,
		proposal.MaterialCount,
		worldID,
		proposal.WorkerNPCID,
		proposal.MaterialCount,
	)
	if err != nil {
		return RepairJob{}, fmt.Errorf(
			"consume repair materials: %w", err,
		)
	}

	affected, err = result.RowsAffected()
	if err != nil {
		return RepairJob{}, fmt.Errorf(
			"read material deduction result: %w", err,
		)
	}
	if affected != 1 {
		return RepairJob{}, fmt.Errorf(
			"repair worker has insufficient materials",
		)
	}

	// 8. 约定费用大于零时，完成付款和收款。
	if proposal.PriceCoins > 0 {
		result, err = tx.ExecContext(ctx, `
			UPDATE npc_inventory
			SET quantity = quantity - ?,
				revision = revision + 1
			WHERE world_id = ?
				AND npc_id = ?
				AND item_id = 'coin'
				AND quantity >= ?
		`,
			proposal.PriceCoins,
			worldID,
			proposal.PayerNPCID,
			proposal.PriceCoins,
		)
		if err != nil {
			return RepairJob{}, fmt.Errorf(
				"deduct repair payment: %w", err,
			)
		}

		affected, err = result.RowsAffected()
		if err != nil {
			return RepairJob{}, fmt.Errorf(
				"read repair payment result: %w", err,
			)
		}
		if affected != 1 {
			return RepairJob{}, fmt.Errorf(
				"repair payer has insufficient coins",
			)
		}

		// 当前初始化已经为维修者创建 coin 库存行。
		// 同时避免余额相加超出 int64 范围。
		const maxQuantity int64 = 1<<63 - 1

		result, err = tx.ExecContext(ctx, `
			UPDATE npc_inventory
			SET quantity = quantity + ?,
				revision = revision + 1
			WHERE world_id = ?
				AND npc_id = ?
				AND item_id = 'coin'
				AND quantity <= ?
		`,
			proposal.PriceCoins,
			worldID,
			proposal.WorkerNPCID,
			maxQuantity-proposal.PriceCoins,
		)
		if err != nil {
			return RepairJob{}, fmt.Errorf(
				"credit repair payment: %w", err,
			)
		}

		affected, err = result.RowsAffected()
		if err != nil {
			return RepairJob{}, fmt.Errorf(
				"read repair credit result: %w", err,
			)
		}
		if affected != 1 {
			return RepairJob{}, fmt.Errorf(
				"worker coin inventory is missing or would overflow",
			)
		}
	}

	// 9. 创建实际维修记录。
	_, err = tx.ExecContext(ctx, `
		INSERT INTO repair_jobs (
			world_id, id, cart_id, worker_npc_id,
			status, started_at, finish_at, revision
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`,
		job.WorldID,
		job.ID,
		job.CartID,
		job.WorkerNPCID,
		job.Status,
		job.StartedAt.Format(time.RFC3339Nano),
		job.FinishAt.Format(time.RFC3339Nano),
		job.Revision,
	)
	if err != nil {
		return RepairJob{}, fmt.Errorf(
			"create repair job: %w", err,
		)
	}

	// 10. 标记提议已经执行，并关联维修记录。
	result, err = tx.ExecContext(ctx, `
		UPDATE repair_proposals
		SET status = ?,
			repair_job_id = ?,
			revision = revision + 1
		WHERE world_id = ?
			AND id = ?
			AND revision = ?
			AND status = ?
			AND payer_confirmed = 1
			AND worker_confirmed = 1
	`,
		RepairProposalStatusExecuted,
		job.ID,
		worldID,
		proposalID,
		expectedRevision,
		RepairProposalStatusConfirmed,
	)
	if err != nil {
		return RepairJob{}, fmt.Errorf(
			"mark repair proposal executed: %w", err,
		)
	}

	affected, err = result.RowsAffected()
	if err != nil {
		return RepairJob{}, fmt.Errorf(
			"read proposal execution result: %w", err,
		)
	}
	if affected != 1 {
		return RepairJob{}, ErrRepairProposalRevisionConflict
	}

	// 11. 更新世界版本。
	_, err = tx.ExecContext(ctx, `
		UPDATE worlds
		SET revision = revision + 1
		WHERE id = ?
	`, worldID)
	if err != nil {
		return RepairJob{}, fmt.Errorf(
			"update repair world revision: %w", err,
		)
	}

	// 全部成功后才提交。
	if err := tx.Commit(); err != nil {
		return RepairJob{}, fmt.Errorf(
			"commit cart repair: %w", err,
		)
	}

	return job, nil
}
