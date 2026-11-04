package negotiation

import (
	"context"
	"cybertown/internal/game/repair"
	"database/sql"
)

type MeetingParticipantView struct {
	NPCID         string `json:"npc_id"`
	InviteStatus  string `json:"invite_status"`
	ArrivalStatus string `json:"arrival_status"`
}

type MeetingView struct {
	ID                string                   `json:"id"`
	Status            string                   `json:"status"`
	InitialProposalID string                   `json:"initial_proposal_id"`
	CurrentProposalID string                   `json:"current_proposal_id"`
	Revision          int64                    `json:"revision"`
	NextActorID       string                   `json:"next_actor_id"`
	EndReason         string                   `json:"end_reason"`
	RoundCount        int                      `json:"round_count"`
	DirectorCalls     int                      `json:"director_calls"`
	Participants      []MeetingParticipantView `json:"participants"`

	Proposal  repair.RepairProposal `json:"proposal"`
	Delivered bool                  `json:"delivered"`
	Messages  []DiscussionLine      `json:"messages"`
}

func GetMeetingView(
	ctx context.Context,
	db *sql.DB,
	worldID string,
	playerID string,
	negotiationID string,
) (MeetingView, error) {
	view := MeetingView{
		Participants: make([]MeetingParticipantView, 0),
	}

	// 在同一个事务中读取，避免会话和参与者来自不同时间点。
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return MeetingView{}, err
	}
	defer tx.Rollback()

	err = tx.QueryRowContext(ctx, `
	SELECT id, status, initial_proposal_id, current_proposal_id,
		revision, COALESCE(next_actor_id, ''), end_reason,
		round_count, director_calls
	FROM negotiations
	WHERE world_id = ? AND player_id = ? AND id = ?
`, worldID, playerID, negotiationID).Scan(
		&view.ID,
		&view.Status,
		&view.InitialProposalID,
		&view.CurrentProposalID,
		&view.Revision,
		&view.NextActorID,
		&view.EndReason,
		&view.RoundCount,
		&view.DirectorCalls,
	)
	if err != nil {
		return MeetingView{}, err
	}

	rows, err := tx.QueryContext(ctx, `
		SELECT npc_id, invite_status, arrival_status
		FROM negotiation_participants
		WHERE world_id = ? AND negotiation_id = ?
		ORDER BY npc_id
	`, worldID, negotiationID)
	if err != nil {
		return MeetingView{}, err
	}

	for rows.Next() {
		var participant MeetingParticipantView
		if err := rows.Scan(
			&participant.NPCID,
			&participant.InviteStatus,
			&participant.ArrivalStatus,
		); err != nil {
			rows.Close()
			return MeetingView{}, err
		}

		view.Participants = append(view.Participants, participant)
	}

	rowsErr := rows.Err()
	rows.Close()
	if rowsErr != nil {
		return MeetingView{}, rowsErr
	}

	view.Proposal, err = repair.GetRepairProposal(
		ctx, tx, worldID, view.CurrentProposalID,
	)
	if err != nil {
		return MeetingView{}, err
	}

	view.Messages, err = discussionHistoryTx(
		ctx, tx, worldID, negotiationID,
	)
	if err != nil {
		return MeetingView{}, err
	}

	err = tx.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM repair_proposal_deliveries
			WHERE world_id = ? AND proposal_id = ?
				AND player_id = ? AND recipient_npc_id = 'merchant'
				AND received_at IS NOT NULL
		)
	`, worldID, view.CurrentProposalID, playerID).Scan(&view.Delivered)
	if err != nil {
		return MeetingView{}, err
	}

	if err := tx.Commit(); err != nil {
		return MeetingView{}, err
	}

	return view, nil
}
