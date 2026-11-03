package game

import (
	"context"
	"database/sql"
	"fmt"
)

func GetRepairProposal(
	ctx context.Context,
	db *sql.DB,
	worldID string,
	proposalID string,
) (RepairProposal, error) {
	if worldID == "" || proposalID == "" {
		return RepairProposal{}, fmt.Errorf(
			"world id and proposal id are required",
		)
	}

	var proposal RepairProposal

	err := db.QueryRowContext(ctx, `
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
		return RepairProposal{}, fmt.Errorf(
			"get repair proposal %q: %w",
			proposalID,
			err,
		)
	}

	return proposal, nil
}
