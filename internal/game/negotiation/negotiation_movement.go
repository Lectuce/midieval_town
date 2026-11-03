package negotiation

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"time"
)

type meetingMoveJob struct {
	ID                  string
	NegotiationID       string
	NPCID               string
	SceneID             string
	Floor               int
	Speed               float64
	StartedGameTime     string
	RouteJSON           string
	ExpectedNPCRevision int64
	Revision            int64
}

// 沿折线路线计算位置，不直接向最终目的地画直线。
func PositionOnRoute(
	points []Position,
	distance float64,
) (Position, bool, error) {
	if len(points) < 2 {
		return Position{}, false, fmt.Errorf("route needs at least two points")
	}
	if math.IsNaN(distance) || math.IsInf(distance, 0) {
		return Position{}, false, fmt.Errorf("invalid movement distance")
	}

	for _, p := range points {
		if math.IsNaN(p.X) || math.IsNaN(p.Y) ||
			math.IsInf(p.X, 0) || math.IsInf(p.Y, 0) {
			return Position{}, false, fmt.Errorf("invalid route coordinate")
		}
	}

	if distance <= 0 {
		return points[0], false, nil
	}

	for i := 1; i < len(points); i++ {
		start := points[i-1]
		end := points[i]

		dx := end.X - start.X
		dy := end.Y - start.Y
		length := math.Hypot(dx, dy)

		if length == 0 {
			continue
		}

		if distance < length {
			ratio := distance / length

			return Position{
				X: start.X + dx*ratio,
				Y: start.Y + dy*ratio,
			}, false, nil
		}

		distance -= length
	}

	return points[len(points)-1], true, nil
}

// 在 AdvanceWorldTime 已开启的事务内调用。
// 此方法不 BeginTx、不 Commit，也不申请另一条数据库连接。
func advanceNegotiationMovementTx(
	ctx context.Context,
	tx *sql.Tx,
	worldID string,
	gameTime time.Time,
) error {
	rows, err := tx.QueryContext(ctx, `
		SELECT
			id, negotiation_id, npc_id,
			scene_id, floor, speed,
			started_game_time, route_json,
			expected_npc_revision, revision
		FROM npc_move_jobs
		WHERE world_id = ? AND status = 'running'
		ORDER BY id
	`, worldID)
	if err != nil {
		return err
	}

	var jobs []meetingMoveJob

	for rows.Next() {
		var job meetingMoveJob

		if err := rows.Scan(
			&job.ID,
			&job.NegotiationID,
			&job.NPCID,
			&job.SceneID,
			&job.Floor,
			&job.Speed,
			&job.StartedGameTime,
			&job.RouteJSON,
			&job.ExpectedNPCRevision,
			&job.Revision,
		); err != nil {
			rows.Close()
			return err
		}

		jobs = append(jobs, job)
	}

	rowsErr := rows.Err()
	rows.Close()
	if rowsErr != nil {
		return rowsErr
	}

	for _, job := range jobs {
		if err := advanceOneMeetingMove(
			ctx, tx, worldID, gameTime, job,
		); err != nil {
			return fmt.Errorf("advance move %s: %w", job.ID, err)
		}
	}

	return nil
}

func advanceOneMeetingMove(
	ctx context.Context,
	tx *sql.Tx,
	worldID string,
	gameTime time.Time,
	job meetingMoveJob,
) error {
	n, err := getNegotiationTx(
		ctx, tx, worldID, job.NegotiationID,
	)
	if err != nil {
		return err
	}

	if n.Status != NegotiationStatusTravelling {
		return stopMeetingMoveTx(
			ctx, tx, worldID, n, job,
			"会话已不处于前往会面阶段。",
		)
	}

	if job.NPCID != "merchant" {
		return stopMeetingMoveTx(
			ctx, tx, worldID, n, job,
			"当前会面路线仅支持马丁前往铁匠铺。",
		)
	}

	var character NPC

	err = tx.QueryRowContext(ctx, `
		SELECT
			scene_id, floor, position_x, position_y,
			current_activity, revision
		FROM npcs
		WHERE world_id = ? AND id = ?
	`, worldID, job.NPCID).Scan(
		&character.SceneID,
		&character.Floor,
		&character.Position.X,
		&character.Position.Y,
		&character.CurrentActivity,
		&character.Revision,
	)
	if err != nil {
		return err
	}

	if character.Revision != job.ExpectedNPCRevision ||
		character.SceneID != job.SceneID ||
		character.Floor != job.Floor ||
		character.CurrentActivity != "walking_to_meeting" {
		return stopMeetingMoveTx(
			ctx, tx, worldID, n, job,
			"马丁的位置或活动已改变，原移动任务停止。",
		)
	}

	// 检查罗兰仍在原地等候，不能只相信之前的 arrived。
	var smithPosition Position
	var smithScene, smithActivity, smithRoute string
	var smithFloor int
	var smithRevision, smithExpectedRevision int64

	err = tx.QueryRowContext(ctx, `
		SELECT
			c.position_x, c.position_y,
			c.scene_id, c.floor,
			c.current_activity, c.revision,
			j.expected_npc_revision, j.route_json
		FROM npc_move_jobs AS j
		JOIN npcs AS c
			ON c.world_id = j.world_id AND c.id = j.npc_id
		WHERE j.world_id = ?
			AND j.negotiation_id = ?
			AND j.npc_id = 'blacksmith'
			AND j.status = 'arrived'
	`, worldID, n.ID).Scan(
		&smithPosition.X,
		&smithPosition.Y,
		&smithScene,
		&smithFloor,
		&smithActivity,
		&smithRevision,
		&smithExpectedRevision,
		&smithRoute,
	)
	if err == sql.ErrNoRows {
		return stopMeetingMoveTx(
			ctx, tx, worldID, n, job,
			"罗兰的到场记录不存在。",
		)
	}
	if err != nil {
		return err
	}

	var smithPoints []Position
	if err := json.Unmarshal([]byte(smithRoute), &smithPoints); err != nil {
		return err
	}
	if len(smithPoints) != 1 {
		return fmt.Errorf("invalid blacksmith meeting stand")
	}

	if smithRevision != smithExpectedRevision ||
		smithScene != job.SceneID ||
		smithFloor != job.Floor ||
		smithActivity != "waiting_for_meeting" ||
		math.Hypot(
			smithPosition.X-smithPoints[0].X,
			smithPosition.Y-smithPoints[0].Y,
		) > 2 {
		return stopMeetingMoveTx(
			ctx, tx, worldID, n, job,
			"罗兰已离开会面站位或开始其他活动。",
		)
	}

	// 报价、参与意愿及行动条件必须仍然有效。
	var valid bool

	err = tx.QueryRowContext(ctx, `
		SELECT
			EXISTS (
				SELECT 1
				FROM repair_proposals AS p
				JOIN carts AS c
					ON c.world_id = p.world_id AND c.id = p.cart_id
				WHERE p.world_id = ?
					AND p.id = ?
					AND p.revision = ?
					AND p.cart_id = ?
					AND p.payer_npc_id = 'merchant'
					AND p.worker_npc_id = 'blacksmith'
					AND p.status = 'proposed'
					AND p.worker_confirmed = 1
					AND p.payer_confirmed = 0
					AND c.repair_status = 'damaged'
			)
			AND (
				SELECT COUNT(*)
				FROM negotiation_participants
				WHERE world_id = ? AND negotiation_id = ?
			) = 2
			AND (
				SELECT COUNT(*)
				FROM negotiation_participants
				WHERE world_id = ?
					AND negotiation_id = ?
					AND invite_status = 'accepted'
					AND (
						(npc_id = 'merchant' AND arrival_status = 'travelling')
						OR
						(npc_id = 'blacksmith' AND arrival_status = 'arrived')
					)
			) = 2
			AND NOT EXISTS (
				SELECT 1 FROM repair_jobs
				WHERE world_id = ? AND status = 'running'
					AND (
						cart_id = ?
						OR worker_npc_id IN ('merchant', 'blacksmith')
					)
			)
			AND NOT EXISTS (
				SELECT 1 FROM transport_jobs
				WHERE world_id = ? AND cart_id = ?
					AND status = 'in_transit'
			)
	`,
		worldID, n.CurrentProposalID,
		n.CurrentProposalRevision, n.CartID,
		worldID, n.ID,
		worldID, n.ID,
		worldID, n.CartID,
		worldID, n.CartID,
	).Scan(&valid)
	if err != nil {
		return err
	}
	if !valid {
		return stopMeetingMoveTx(
			ctx, tx, worldID, n, job,
			"报价、参与意愿或行动条件发生变化，会面停止。",
		)
	}

	var points []Position
	if err := json.Unmarshal([]byte(job.RouteJSON), &points); err != nil {
		return err
	}

	startedAt, err := time.Parse(time.RFC3339Nano, job.StartedGameTime)
	if err != nil {
		return err
	}
	if gameTime.Before(startedAt) {
		return fmt.Errorf("world time is earlier than movement start")
	}
	if job.Speed <= 0 || math.IsNaN(job.Speed) || math.IsInf(job.Speed, 0) {
		return fmt.Errorf("invalid movement speed")
	}

	position, arrived, err := PositionOnRoute(
		points,
		gameTime.Sub(startedAt).Seconds()*job.Speed,
	)
	if err != nil {
		return err
	}

	if arrived {
		distance := math.Hypot(
			position.X-smithPosition.X,
			position.Y-smithPosition.Y,
		)
		if distance < 18 || distance > 32 {
			return stopMeetingMoveTx(
				ctx, tx, worldID, n, job,
				"双方实际站位不满足会面距离。",
			)
		}
	}

	if position == character.Position && !arrived {
		return nil
	}

	activity := "walking_to_meeting"
	jobStatus := "running"

	if arrived {
		activity = "attending_meeting"
		jobStatus = "arrived"
	}

	if err := meetingUpdateOne(ctx, tx, `
		UPDATE npcs
		SET position_x = ?, position_y = ?,
			current_activity = ?, revision = revision + 1
		WHERE world_id = ? AND id = ? AND revision = ?
	`,
		position.X, position.Y, activity,
		worldID, job.NPCID, job.ExpectedNPCRevision,
	); err != nil {
		return err
	}

	if err := meetingUpdateOne(ctx, tx, `
		UPDATE npc_move_jobs
		SET expected_npc_revision = ?,
			status = ?, revision = revision + 1
		WHERE world_id = ? AND id = ?
			AND status = 'running' AND revision = ?
	`,
		job.ExpectedNPCRevision+1,
		jobStatus,
		worldID, job.ID, job.Revision,
	); err != nil {
		return err
	}

	if !arrived {
		return nil
	}

	if err := meetingUpdateOne(ctx, tx, `
		UPDATE negotiation_participants
		SET arrival_status = 'arrived'
		WHERE world_id = ? AND negotiation_id = ?
			AND npc_id = 'merchant'
			AND invite_status = 'accepted'
			AND arrival_status = 'travelling'
	`, worldID, n.ID); err != nil {
		return err
	}

	if err := meetingUpdateOne(ctx, tx, `
		UPDATE npcs
		SET current_activity = 'attending_meeting',
			revision = revision + 1
		WHERE world_id = ? AND id = 'blacksmith'
			AND revision = ?
	`, worldID, smithRevision); err != nil {
		return err
	}

	if err := meetingUpdateOne(ctx, tx, `
		UPDATE npc_move_jobs
		SET expected_npc_revision = ?, revision = revision + 1
		WHERE world_id = ? AND negotiation_id = ?
			AND npc_id = 'blacksmith' AND status = 'arrived'
	`, smithRevision+1, worldID, n.ID); err != nil {
		return err
	}

	if err := meetingUpdateOne(ctx, tx, `
		UPDATE negotiations
		SET status = 'discussing',
			next_actor_id = 'merchant',
			revision = revision + 1
		WHERE world_id = ? AND id = ?
			AND status = 'travelling' AND revision = ?
	`, worldID, n.ID, n.Revision); err != nil {
		return err
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO negotiation_events (
			world_id, id, negotiation_id,
			actor_kind, actor_id, type,
			negotiation_revision, text, status
		)
		VALUES (?, ?, ?, 'system', 'game_service',
			'arrival', ?, ?, 'done')
	`,
		worldID, n.ID+":arrival:merchant", n.ID,
		n.Revision+1,
		"马丁已到达铁匠铺，双方已经会面。",
	)
	return err
}

// 所有带版本条件的更新都必须恰好命中一条。
// 返回错误会让外层世界时间事务一起回滚。
func meetingUpdateOne(
	ctx context.Context,
	tx *sql.Tx,
	query string,
	args ...any,
) error {
	result, err := tx.ExecContext(ctx, query, args...)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return fmt.Errorf("meeting state changed: affected %d rows", count)
	}
	return nil
}

func stopMeetingMoveTx(
	ctx context.Context,
	tx *sql.Tx,
	worldID string,
	n Negotiation,
	job meetingMoveJob,
	reason string,
) error {
	// 仅在角色版本和会面活动仍匹配时恢复原活动。
	// 不覆盖其他系统刚刚写入的新活动或位置。
	_, err := tx.ExecContext(ctx, `
		UPDATE npcs
		SET current_activity = (
			SELECT j.previous_activity
			FROM npc_move_jobs AS j
			WHERE j.world_id = npcs.world_id
				AND j.negotiation_id = ?
				AND j.npc_id = npcs.id
		),
		revision = revision + 1
		WHERE world_id = ?
			AND current_activity IN (
				'walking_to_meeting', 'waiting_for_meeting'
			)
			AND EXISTS (
				SELECT 1
				FROM npc_move_jobs AS j
				WHERE j.world_id = npcs.world_id
					AND j.negotiation_id = ?
					AND j.npc_id = npcs.id
					AND j.expected_npc_revision = npcs.revision
			)
	`, n.ID, worldID, n.ID)
	if err != nil {
		return err
	}

	if err := meetingUpdateOne(ctx, tx, `
		UPDATE npc_move_jobs
		SET status = 'blocked', revision = revision + 1
		WHERE world_id = ? AND id = ? AND status = 'running'
	`, worldID, job.ID); err != nil {
		return err
	}

	_, err = tx.ExecContext(ctx, `
		UPDATE negotiation_participants
		SET arrival_status = 'not_started'
		WHERE world_id = ? AND negotiation_id = ?
			AND arrival_status = 'travelling'
	`, worldID, n.ID)
	if err != nil {
		return err
	}

	eventRevision := n.Revision

	// 不把已经 cancelled 等终态重新改成 blocked。
	if n.Status == NegotiationStatusTravelling {
		if err := meetingUpdateOne(ctx, tx, `
			UPDATE negotiations
			SET status = 'blocked',
				end_reason = ?,
				next_actor_id = NULL,
				revision = revision + 1
			WHERE world_id = ? AND id = ?
				AND status = 'travelling' AND revision = ?
		`, reason, worldID, n.ID, n.Revision); err != nil {
			return err
		}
		eventRevision++
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO negotiation_events (
			world_id, id, negotiation_id,
			actor_kind, actor_id, type,
			negotiation_revision, text, status
		)
		VALUES (?, ?, ?, 'system', 'game_service',
			'ended', ?, ?, 'done')
	`,
		worldID, job.ID+":stopped", n.ID,
		eventRevision, reason,
	)
	return err
}
