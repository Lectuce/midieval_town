package negotiation

import (
	"context"
	"database/sql"
)

func ListReadyMeetings(
	ctx context.Context,
	db *sql.DB,
) ([]ReadyMeeting, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT n.world_id, n.id, n.revision
		FROM negotiations AS n
		WHERE n.status = 'inviting'
			AND (
				SELECT COUNT(*)
				FROM negotiation_participants AS p
				WHERE p.world_id = n.world_id
					AND p.negotiation_id = n.id
			) = 2
			AND (
				SELECT COUNT(*)
				FROM negotiation_participants AS p
				WHERE p.world_id = n.world_id
					AND p.negotiation_id = n.id
					AND p.npc_id IN ('merchant', 'blacksmith')
					AND p.invite_status = 'accepted'
					AND p.arrival_status = 'not_started'
			) = 2
		ORDER BY n.created_at, n.id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]ReadyMeeting, 0)

	for rows.Next() {
		var meeting ReadyMeeting
		if err := rows.Scan(
			&meeting.WorldID,
			&meeting.ID,
			&meeting.Revision,
		); err != nil {
			return nil, err
		}

		result = append(result, meeting)
	}

	return result, rows.Err()
}

// 当前第一版：出发安排失败就明确结束这次会面，
// 不让会话永久停在“双方已接受”，也不无限自动重试。
// 只有候选会话仍未变化、未创建移动任务时才能结束。
func BlockUnstartedMeeting(
	ctx context.Context,
	db *sql.DB,
	meeting ReadyMeeting,
) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	result, err := tx.ExecContext(ctx, `
		UPDATE negotiations
		SET status = 'blocked',
			next_actor_id = NULL,
			revision = revision + 1
		WHERE world_id = ?
			AND id = ?
			AND revision = ?
			AND status = 'inviting'
			AND NOT EXISTS (
				SELECT 1
				FROM npc_move_jobs AS j
				WHERE j.world_id = negotiations.world_id
					AND j.negotiation_id = negotiations.id
			)
	`, meeting.WorldID, meeting.ID, meeting.Revision)
	if err != nil {
		return err
	}

	count, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if count == 0 {
		return nil
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO negotiation_events (
			world_id, id, negotiation_id,
			actor_kind, actor_id, type,
			negotiation_revision, text, status
		)
		VALUES (
			?, ?, ?,
			'system', 'game_service', 'ended',
			?, ?, 'done'
		)
	`,
		meeting.WorldID,
		meeting.ID+":meeting-blocked",
		meeting.ID,
		meeting.Revision+1,
		"双方未能出发，本次会面已停止。",
	)
	if err != nil {
		return err
	}

	return tx.Commit()
}
