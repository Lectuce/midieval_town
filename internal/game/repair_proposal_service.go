package game

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

var (
	ErrRepairProposalRevisionConflict = errors.New(
		"repair proposal revision conflict",
	)
	ErrRepairProposalNotConfirmable = errors.New(
		"repair proposal cannot be confirmed",
	)
	ErrRepairProposalParticipant = errors.New(
		"NPC is not a participant in this repair proposal",
	)
)

func CreateRepairProposal(
	ctx context.Context,
	db *sql.DB,
	worldID string,
	req CreateRepairProposalRequest,
) (RepairProposal, error) {
	if worldID == "" || req.ID == "" {
		return RepairProposal{}, fmt.Errorf(
			"world id and proposal id are required",
		)
	}

	if req.PriceCoins < 0 {
		return RepairProposal{}, fmt.Errorf(
			"repair price cannot be negative",
		)
	}

	if req.MaterialCount <= 0 {
		return RepairProposal{}, fmt.Errorf(
			"material count must be positive",
		)
	}

	if req.DurationMinutes < 1 || req.DurationMinutes > 1440 {
		return RepairProposal{}, fmt.Errorf(
			"repair duration must be between 1 and 1440 minutes",
		)
	}

	proposal := RepairProposal{
		WorldID:         worldID,
		ID:              req.ID,
		CartID:          AbandonedCartID,
		PayerNPCID:      "merchant",
		WorkerNPCID:     "blacksmith",
		PriceCoins:      req.PriceCoins,
		MaterialCount:   req.MaterialCount,
		DurationMinutes: req.DurationMinutes,
		Status:          RepairProposalStatusProposed,
		Revision:        1,
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return RepairProposal{}, fmt.Errorf(
			"begin repair proposal: %w", err,
		)
	}
	defer tx.Rollback()

	// 只针对当前仍然损坏的货车创建提议。
	result, err := tx.ExecContext(ctx, `
		INSERT INTO repair_proposals (
			world_id, id, cart_id,
			payer_npc_id, worker_npc_id,
			price_coins, material_count, duration_minutes,
			payer_confirmed, worker_confirmed,
			status, revision
		)
		SELECT
			?, ?, id,
			?, ?,
			?, ?, ?,
			0, 0,
			?, 1
		FROM carts
		WHERE world_id = ?
			AND id = ?
			AND repair_status = ?
	`,
		proposal.WorldID,
		proposal.ID,
		proposal.PayerNPCID,
		proposal.WorkerNPCID,
		proposal.PriceCoins,
		proposal.MaterialCount,
		proposal.DurationMinutes,
		proposal.Status,
		worldID,
		proposal.CartID,
		CartStatusDamaged,
	)
	if err != nil {
		return RepairProposal{}, fmt.Errorf(
			"create repair proposal: %w", err,
		)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return RepairProposal{}, fmt.Errorf(
			"read repair proposal insert result: %w", err,
		)
	}
	if affected == 0 {
		return RepairProposal{}, fmt.Errorf(
			"cart is missing or does not require repair",
		)
	}

	_, err = tx.ExecContext(ctx, `
		UPDATE worlds
		SET revision = revision + 1
		WHERE id = ?
	`, worldID)
	if err != nil {
		return RepairProposal{}, fmt.Errorf(
			"update world proposal revision: %w", err,
		)
	}

	if err := tx.Commit(); err != nil {
		return RepairProposal{}, fmt.Errorf(
			"commit repair proposal: %w", err,
		)
	}

	return proposal, nil
}

// actorNPCID 必须由服务端可信的角色上下文提供，
// 不能直接使用玩家请求或模型工具参数里的 NPC ID。
func ConfirmRepairProposal(
	ctx context.Context,
	db *sql.DB,
	worldID string,
	proposalID string,
	actorNPCID string,
	expectedRevision int64,
) (RepairProposal, error) {
	if actorNPCID == "" {
		return RepairProposal{}, fmt.Errorf(
			"confirming NPC id is required",
		)
	}
	if expectedRevision < 1 {
		return RepairProposal{}, fmt.Errorf(
			"expected revision must be positive",
		)
	}

	// 在开启事务之前读取，避免占用唯一连接后再调用 db 查询。
	proposal, err := GetRepairProposal(
		ctx, db, worldID, proposalID,
	)
	if err != nil {
		return RepairProposal{}, err
	}

	if actorNPCID != proposal.PayerNPCID &&
		actorNPCID != proposal.WorkerNPCID {
		return RepairProposal{}, ErrRepairProposalParticipant
	}

	if proposal.Revision != expectedRevision {
		return RepairProposal{}, fmt.Errorf(
			"%w: expected %d, current %d",
			ErrRepairProposalRevisionConflict,
			expectedRevision,
			proposal.Revision,
		)
	}

	if proposal.Status != RepairProposalStatusProposed &&
		proposal.Status != RepairProposalStatusConfirmed {
		return RepairProposal{}, ErrRepairProposalNotConfirmable
	}

	// 同一参与者已经确认过，无需再次更新。
	if actorNPCID == proposal.PayerNPCID {
		if proposal.PayerConfirmed {
			return proposal, nil
		}
		proposal.PayerConfirmed = true
	} else {
		if proposal.WorkerConfirmed {
			return proposal, nil
		}
		proposal.WorkerConfirmed = true
	}

	if proposal.PayerConfirmed && proposal.WorkerConfirmed {
		proposal.Status = RepairProposalStatusConfirmed
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return RepairProposal{}, fmt.Errorf(
			"begin repair confirmation: %w", err,
		)
	}
	defer tx.Rollback()

	// 更新时再次检查版本，防止覆盖其他参与者刚完成的确认。
	result, err := tx.ExecContext(ctx, `
		UPDATE repair_proposals
		SET payer_confirmed = ?,
			worker_confirmed = ?,
			status = ?,
			revision = revision + 1
		WHERE world_id = ?
			AND id = ?
			AND revision = ?
			AND status = ?
	`,
		proposal.PayerConfirmed,
		proposal.WorkerConfirmed,
		proposal.Status,
		worldID,
		proposalID,
		expectedRevision,
		RepairProposalStatusProposed,
	)
	if err != nil {
		return RepairProposal{}, fmt.Errorf(
			"confirm repair proposal: %w", err,
		)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return RepairProposal{}, fmt.Errorf(
			"read repair confirmation result: %w", err,
		)
	}
	if affected == 0 {
		return RepairProposal{}, ErrRepairProposalRevisionConflict
	}

	_, err = tx.ExecContext(ctx, `
		UPDATE worlds
		SET revision = revision + 1
		WHERE id = ?
	`, worldID)
	if err != nil {
		return RepairProposal{}, fmt.Errorf(
			"update world confirmation revision: %w", err,
		)
	}

	if err := tx.Commit(); err != nil {
		return RepairProposal{}, fmt.Errorf(
			"commit repair confirmation: %w", err,
		)
	}

	proposal.Revision++

	return proposal, nil
}
