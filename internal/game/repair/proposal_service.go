package repair

import (
	"context"
	"database/sql"
	"fmt"

	constError "cybertown/internal/game/const_error"
	"cybertown/internal/game/transport"
)

func CreateRepairProposal(
	ctx context.Context,
	db *sql.DB,
	worldID string,
	req CreateRepairProposalRequest,
) (RepairProposal, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return RepairProposal{}, err
	}
	defer tx.Rollback()

	proposal, err := CreateRepairProposalTx(ctx, tx, worldID, req, "")
	if err != nil {
		return RepairProposal{}, err
	}

	if err := tx.Commit(); err != nil {
		return RepairProposal{}, err
	}
	return proposal, nil
}

// negotiationID 只能由服务端协商执行器提供，不能来自模型参数。
func CreateRepairProposalTx(
	ctx context.Context,
	tx *sql.Tx,
	worldID string,
	req CreateRepairProposalRequest,
	negotiationID string,
) (RepairProposal, error) {
	if worldID == "" || req.ID == "" {
		return RepairProposal{}, fmt.Errorf("world id and proposal id are required")
	}
	if req.PriceCoins < 0 || req.MaterialCount <= 0 ||
		req.DurationMinutes < 1 || req.DurationMinutes > 1440 {
		return RepairProposal{}, fmt.Errorf("invalid repair terms")
	}

	if err := CheckRepairNegotiation(
		ctx, tx, worldID, transport.AbandonedCartID, negotiationID,
	); err != nil {
		return RepairProposal{}, err
	}

	p := RepairProposal{
		WorldID:         worldID,
		ID:              req.ID,
		CartID:          transport.AbandonedCartID,
		PayerNPCID:      "merchant",
		WorkerNPCID:     "blacksmith",
		PriceCoins:      req.PriceCoins,
		MaterialCount:   req.MaterialCount,
		DurationMinutes: req.DurationMinutes,
		Status:          RepairProposalStatusProposed,
		Revision:        1,
	}

	result, err := tx.ExecContext(ctx, `
		INSERT INTO repair_proposals (
			world_id, id, cart_id,
			payer_npc_id, worker_npc_id,
			price_coins, material_count, duration_minutes,
			payer_confirmed, worker_confirmed, status, revision
		)
		SELECT ?, ?, id, ?, ?, ?, ?, ?, 0, 0, 'proposed', 1
		FROM carts
		WHERE world_id = ? AND id = ? AND repair_status = 'damaged'
	`,
		p.WorldID, p.ID, p.PayerNPCID, p.WorkerNPCID,
		p.PriceCoins, p.MaterialCount, p.DurationMinutes,
		worldID, p.CartID,
	)
	if err != nil {
		return RepairProposal{}, err
	}

	count, err := result.RowsAffected()
	if err != nil {
		return RepairProposal{}, err
	}
	if count != 1 {
		return RepairProposal{}, fmt.Errorf("cart does not require repair")
	}

	_, err = tx.ExecContext(ctx, `
		UPDATE worlds SET revision = revision + 1 WHERE id = ?
	`, worldID)
	if err != nil {
		return RepairProposal{}, err
	}

	return p, nil
}

func ConfirmRepairProposal(
	ctx context.Context,
	db *sql.DB,
	worldID string,
	proposalID string,
	actorNPCID string,
	expectedRevision int64,
) (RepairProposal, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return RepairProposal{}, err
	}
	defer tx.Rollback()

	p, err := ConfirmRepairProposalTx(
		ctx, tx, worldID, proposalID,
		actorNPCID, expectedRevision, "",
	)
	if err != nil {
		return RepairProposal{}, err
	}

	if err := tx.Commit(); err != nil {
		return RepairProposal{}, err
	}
	return p, nil
}

func ConfirmRepairProposalTx(
	ctx context.Context,
	tx *sql.Tx,
	worldID string,
	proposalID string,
	actorNPCID string,
	expectedRevision int64,
	negotiationID string,
) (RepairProposal, error) {
	if actorNPCID == "" || expectedRevision < 1 {
		return RepairProposal{}, fmt.Errorf("invalid confirmation")
	}

	p, err := GetRepairProposal(ctx, tx, worldID, proposalID)
	if err != nil {
		return RepairProposal{}, err
	}

	if err := CheckRepairNegotiation(
		ctx, tx, worldID, p.CartID, negotiationID,
	); err != nil {
		return RepairProposal{}, err
	}

	if actorNPCID != p.PayerNPCID && actorNPCID != p.WorkerNPCID {
		return RepairProposal{}, constError.ErrRepairProposalParticipant
	}
	if p.Revision != expectedRevision {
		return RepairProposal{}, constError.ErrRepairProposalRevisionConflict
	}
	if p.Status != RepairProposalStatusProposed &&
		p.Status != RepairProposalStatusConfirmed {
		return RepairProposal{}, constError.ErrRepairProposalNotConfirmable
	}

	if actorNPCID == p.PayerNPCID {
		var received bool
		err = tx.QueryRowContext(ctx, `
			SELECT EXISTS (
				SELECT 1
				FROM repair_proposal_deliveries
				WHERE world_id = ?
					AND proposal_id = ?
					AND recipient_npc_id = ?
					AND received_at IS NOT NULL
			)
		`, worldID, proposalID, actorNPCID).Scan(&received)
		if err != nil {
			return RepairProposal{}, err
		}
		if !received {
			return RepairProposal{}, constError.ErrRepairProposalDeliveryDenied
		}

		if p.PayerConfirmed {
			return p, nil
		}
		p.PayerConfirmed = true
	} else {
		if p.WorkerConfirmed {
			return p, nil
		}
		p.WorkerConfirmed = true
	}

	if p.PayerConfirmed && p.WorkerConfirmed {
		p.Status = RepairProposalStatusConfirmed
	}

	result, err := tx.ExecContext(ctx, `
		UPDATE repair_proposals
		SET payer_confirmed = ?,
			worker_confirmed = ?,
			status = ?,
			revision = revision + 1
		WHERE world_id = ? AND id = ?
			AND revision = ? AND status = 'proposed'
	`,
		p.PayerConfirmed, p.WorkerConfirmed, p.Status,
		worldID, proposalID, expectedRevision,
	)
	if err != nil {
		return RepairProposal{}, err
	}

	count, err := result.RowsAffected()
	if err != nil {
		return RepairProposal{}, err
	}
	if count != 1 {
		return RepairProposal{}, constError.ErrRepairProposalRevisionConflict
	}

	_, err = tx.ExecContext(ctx, `
		UPDATE worlds SET revision = revision + 1 WHERE id = ?
	`, worldID)
	if err != nil {
		return RepairProposal{}, err
	}

	p.Revision++
	return p, nil
}
