package negotiation

import (
	"context"
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
	CurrentProposalID string                   `json:"current_proposal_id"`
	Revision          int64                    `json:"revision"`
	Participants      []MeetingParticipantView `json:"participants"`
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
		SELECT id, status, current_proposal_id, revision
		FROM negotiations
		WHERE world_id = ?
			AND player_id = ?
			AND id = ?
	`, worldID, playerID, negotiationID).Scan(
		&view.ID,
		&view.Status,
		&view.CurrentProposalID,
		&view.Revision,
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

	if err := tx.Commit(); err != nil {
		return MeetingView{}, err
	}

	return view, nil
}
