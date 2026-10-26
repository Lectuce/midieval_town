package game

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

var ErrRepairProposalDeliveryDenied = errors.New(
	"repair proposal cannot be delivered",
)

// 玩家明确转交已经取得的报价。
// 只记录信息送达，不替接收人确认。
func DeliverRepairProposal(
	ctx context.Context,
	db *sql.DB,
	worldID string,
	playerID string,
	proposalID string,
	recipientNPCID string,
) (RepairProposal, error) {
	if worldID == "" || playerID == "" ||
		proposalID == "" || recipientNPCID == "" {
		return RepairProposal{}, ErrRepairProposalDeliveryDenied
	}

	// 一个 UPDATE 同时检查：
	// 玩家取得过报价、玩家有效、收件人正确、报价仍可交涉。
	result, err := db.ExecContext(ctx, `
		UPDATE repair_proposal_deliveries
		SET received_at = COALESCE(
			received_at,
			CURRENT_TIMESTAMP
		)
		WHERE world_id = ?
			AND proposal_id = ?
			AND player_id = ?
			AND recipient_npc_id = ?
			AND EXISTS (
				SELECT 1
				FROM players AS pl
				WHERE pl.world_id =
					repair_proposal_deliveries.world_id
					AND pl.id =
						repair_proposal_deliveries.player_id
					AND pl.status = 'active'
			)
			AND EXISTS (
				SELECT 1
				FROM repair_proposals AS p
				WHERE p.world_id =
					repair_proposal_deliveries.world_id
					AND p.id =
						repair_proposal_deliveries.proposal_id
					AND p.payer_npc_id =
						repair_proposal_deliveries.recipient_npc_id
					AND p.worker_confirmed = 1
					AND p.status IN ('proposed', 'confirmed')
			)
	`,
		worldID,
		proposalID,
		playerID,
		recipientNPCID,
	)
	if err != nil {
		return RepairProposal{}, fmt.Errorf(
			"deliver repair proposal: %w", err,
		)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return RepairProposal{}, err
	}
	if affected == 0 {
		return RepairProposal{}, ErrRepairProposalDeliveryDenied
	}

	return GetRepairProposal(ctx, db, worldID, proposalID)
}

// NPC 只能查询确实送达给自己的报价。
func GetReceivedRepairProposals(
	ctx context.Context,
	db *sql.DB,
	worldID string,
	npcID string,
) ([]RepairProposal, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT proposal_id
		FROM repair_proposal_deliveries
		WHERE world_id = ?
			AND recipient_npc_id = ?
			AND received_at IS NOT NULL
		GROUP BY proposal_id
		ORDER BY MAX(received_at) DESC, proposal_id
		LIMIT 20
	`, worldID, npcID)
	if err != nil {
		return nil, fmt.Errorf(
			"query received repair proposals: %w", err,
		)
	}

	var ids []string

	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}

	rowsErr := rows.Err()
	rows.Close()

	if rowsErr != nil {
		return nil, rowsErr
	}

	// 当前数据库只有一个连接。
	// 必须关闭 rows，再调用 GetRepairProposal。
	proposals := make([]RepairProposal, 0, len(ids))

	for _, id := range ids {
		proposal, err := GetRepairProposal(
			ctx, db, worldID, id,
		)
		if err != nil {
			return nil, err
		}
		proposals = append(proposals, proposal)
	}

	return proposals, nil
}
