package negotiation

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

func AddDiscussionMessage(
	ctx context.Context,
	db *sql.DB,
	worldID, playerID, negotiationID, requestID, message string,
) error {
	message = strings.TrimSpace(message)
	if requestID == "" || len(requestID) > 128 || !discussionTextOK(message) {
		return ErrDiscussionInput
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	n, err := getNegotiationTx(ctx, tx, worldID, negotiationID)
	if err != nil {
		return err
	}
	if n.PlayerID != playerID {
		return sql.ErrNoRows
	}

	eventID := discussionID(n.ID+"/"+playerID+"/"+requestID, "player")
	var previous, status string
	err = tx.QueryRowContext(ctx, `
		SELECT text, status FROM negotiation_events
		WHERE world_id = ? AND id = ?
	`, worldID, eventID).Scan(&previous, &status)
	if err == nil {
		if previous != message {
			return ErrDiscussionInput
		}
		if status == "cancelled" || status == "failed" {
			return ErrDiscussionState
		}
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}

	if _, err := discussionReadyTx(ctx, tx, n); err != nil {
		return err
	}

	var near bool
	err = tx.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM players pl
			JOIN npcs a ON a.world_id = pl.world_id AND a.id = 'blacksmith'
			WHERE pl.world_id = ? AND pl.id = ?
				AND pl.status = 'active'
				AND pl.scene_id = a.scene_id AND pl.floor = a.floor
				AND (
					(pl.position_x - a.position_x) * (pl.position_x - a.position_x)
					+ (pl.position_y - a.position_y) * (pl.position_y - a.position_y)
				) <= 4096
		)
	`, worldID, playerID).Scan(&near)
	if err != nil {
		return err
	}
	if !near {
		return ErrDiscussionState
	}

	var count int
	err = tx.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM negotiation_events
		WHERE world_id = ? AND negotiation_id = ? AND type = 'player_message'
	`, worldID, n.ID).Scan(&count)
	if err != nil {
		return err
	}
	if count >= 8 {
		return ErrDiscussionInput
	}

	if err := insertDiscussionEventTx(
		ctx, tx, n, eventID, playerID, "",
		"player_message", "", message,
		"pending", "player", "", false, discussionPayload{},
	); err != nil {
		return err
	}

	return tx.Commit()
}

func CancelDiscussion(
	ctx context.Context,
	db *sql.DB,
	worldID, playerID, negotiationID string,
) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	n, err := getNegotiationTx(ctx, tx, worldID, negotiationID)
	if err != nil {
		return err
	}
	if n.PlayerID != playerID {
		return sql.ErrNoRows
	}
	if n.Status == "agreed" {
		return ErrDiscussionState
	}

	if err := endDiscussionTx(
		ctx, tx, n, "cancelled", "玩家结束了本次会面。",
	); err != nil {
		return err
	}
	return tx.Commit()
}

func GetLatestMeeting(
	ctx context.Context,
	db *sql.DB,
	worldID, playerID string,
) (MeetingView, error) {
	var id string
	err := db.QueryRowContext(ctx, `
		SELECT id FROM negotiations
		WHERE world_id = ? AND player_id = ?
		ORDER BY
			CASE WHEN status IN ('inviting', 'travelling', 'discussing')
				THEN 0 ELSE 1 END,
			created_at DESC, rowid DESC
		LIMIT 1
	`, worldID, playerID).Scan(&id)
	if err != nil {
		return MeetingView{}, err
	}
	return GetMeetingView(ctx, db, worldID, playerID, id)
}
