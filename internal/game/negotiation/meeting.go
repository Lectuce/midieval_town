package negotiation

import (
	"context"
	"cybertown/internal/game/actor"
	constError "cybertown/internal/game/const_error"
	"cybertown/internal/game/model"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"time"
)

func LoadNegotiationMeetingRoute(
	path string,
) (NegotiationMeetingRoute, error) {
	var route NegotiationMeetingRoute

	raw, err := os.ReadFile(path)
	if err != nil {
		return route, fmt.Errorf("read meeting route: %w", err)
	}

	if err := json.Unmarshal(raw, &route); err != nil {
		return route, fmt.Errorf("decode meeting route: %w", err)
	}

	if err := ValidateNegotiationMeetingRoute(route); err != nil {
		return route, err
	}

	return route, nil
}

func ValidateNegotiationMeetingRoute(
	route NegotiationMeetingRoute,
) error {
	// 当前只实现同一小镇、地面的固定路线。
	if route.ID == "" ||
		route.SceneID != "town" ||
		route.Floor != 0 {
		return fmt.Errorf("invalid meeting route identity")
	}

	if math.IsNaN(route.Speed) ||
		math.IsInf(route.Speed, 0) ||
		route.Speed <= 0 ||
		route.Speed > 64 {
		return fmt.Errorf("meeting speed must be between 0 and 64")
	}

	if len(route.MerchantPoints) < 2 ||
		len(route.MerchantPoints) > 128 {
		return fmt.Errorf("meeting route requires 2 to 128 points")
	}

	points := append(
		[]model.Position{route.BlacksmithStand},
		route.MerchantPoints...,
	)

	for _, point := range points {
		if math.IsNaN(point.X) || math.IsNaN(point.Y) ||
			math.IsInf(point.X, 0) || math.IsInf(point.Y, 0) {
			return fmt.Errorf("meeting coordinates must be finite")
		}
	}

	for i := 1; i < len(route.MerchantPoints); i++ {
		if route.MerchantPoints[i] == route.MerchantPoints[i-1] {
			return fmt.Errorf("adjacent route points cannot overlap")
		}
	}

	end := route.MerchantPoints[len(route.MerchantPoints)-1]

	distance := math.Hypot(
		end.X-route.BlacksmithStand.X,
		end.Y-route.BlacksmithStand.Y,
	)

	// 当前角色脚部约 16 像素宽，保留间隔。
	// 这是本次会面站位的距离约束，不是地图碰撞检测。
	if distance < 18 || distance > 32 {
		return fmt.Errorf("meeting stands must be 18 to 32 units apart")
	}

	return nil
}

func StartNegotiationMeeting(
	ctx context.Context,
	db *sql.DB,
	worldID string,
	negotiationID string,
	route NegotiationMeetingRoute,
) error {
	if db == nil || worldID == "" || negotiationID == "" {
		return fmt.Errorf("database, world and negotiation are required")
	}

	if err := ValidateNegotiationMeetingRoute(route); err != nil {
		return err
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	n, err := getNegotiationTx(ctx, tx, worldID, negotiationID)
	if err != nil {
		return fmt.Errorf("load negotiation: %w", err)
	}

	// 已创建过，不重复安排。
	if n.Status == NegotiationStatusTravelling ||
		n.Status == NegotiationStatusDiscussing {
		var count int

		err := tx.QueryRowContext(ctx, `
			SELECT COUNT(*)
			FROM npc_move_jobs
			WHERE world_id = ?
				AND negotiation_id = ?
				AND npc_id IN ('merchant', 'blacksmith')
		`, worldID, n.ID).Scan(&count)
		if err != nil {
			return err
		}

		if count != 2 {
			return fmt.Errorf("meeting job records are incomplete")
		}

		return nil
	}

	if n.Status != NegotiationStatusInviting {
		return constError.ErrMeetingNotReady
	}

	// 必须正好是两位参与者，并且都已经接受、尚未出发。
	var total, ready int

	err = tx.QueryRowContext(ctx, `
		SELECT
			COUNT(*),
			COALESCE(SUM(
				CASE WHEN
					npc_id IN ('merchant', 'blacksmith')
					AND invite_status = 'accepted'
					AND arrival_status = 'not_started'
				THEN 1 ELSE 0 END
			), 0)
		FROM negotiation_participants
		WHERE world_id = ? AND negotiation_id = ?
	`, worldID, n.ID).Scan(&total, &ready)
	if err != nil {
		return err
	}

	if total != 2 || ready != 2 {
		return constError.ErrMeetingNotReady
	}

	// 报价、玩家和货车都必须仍有效。
	var available bool

	err = tx.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM repair_proposals AS p
			JOIN carts AS c
				ON c.world_id = p.world_id
				AND c.id = p.cart_id
			JOIN players AS pl
				ON pl.world_id = p.world_id
				AND pl.id = ?
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
				AND pl.status = 'active'
		)
	`,
		n.PlayerID,
		worldID,
		n.CurrentProposalID,
		n.CurrentProposalRevision,
		n.CartID,
	).Scan(&available)
	if err != nil {
		return err
	}
	if !available {
		return fmt.Errorf("proposal, player or cart is unavailable")
	}

	// 检查与其他协商、移动、维修和运输的冲突。
	var busy bool

	err = tx.QueryRowContext(ctx, `
		SELECT
			EXISTS (
				SELECT 1
				FROM npc_move_jobs
				WHERE world_id = ?
					AND npc_id IN ('merchant', 'blacksmith')
					AND status = 'running'
			)
			OR EXISTS (
				SELECT 1
				FROM negotiation_participants AS p
				JOIN negotiations AS other
					ON other.world_id = p.world_id
					AND other.id = p.negotiation_id
				WHERE p.world_id = ?
					AND other.id != ?
					AND p.npc_id IN ('merchant', 'blacksmith')
					AND other.status IN (
						'inviting', 'travelling', 'discussing'
					)
			)
			OR EXISTS (
				SELECT 1
				FROM repair_jobs
				WHERE world_id = ?
					AND status = 'running'
					AND (
						cart_id = ?
						OR worker_npc_id IN ('merchant', 'blacksmith')
					)
			)
			OR EXISTS (
				SELECT 1
				FROM transport_jobs
				WHERE world_id = ?
					AND cart_id = ?
					AND status = 'in_transit'
			)
			OR EXISTS (
				SELECT 1
				FROM repair_proposals
				WHERE world_id = ?
					AND cart_id = ?
					AND status = 'confirmed'
			)
	`,
		worldID,
		worldID, n.ID,
		worldID, n.CartID,
		worldID, n.CartID,
		worldID, n.CartID,
	).Scan(&busy)
	if err != nil {
		return err
	}
	if busy {
		return fmt.Errorf("meeting participants or cart are occupied")
	}

	// 在当前事务中读取，不能调用使用 db 的 GetNPC。
	characters := make([]actor.NPC, 2)

	for i, id := range []string{"merchant", "blacksmith"} {
		character := &characters[i]
		character.ID = id

		err = tx.QueryRowContext(ctx, `
			SELECT
				scene_id,
				floor,
				position_x,
				position_y,
				current_activity,
				revision
			FROM npcs
			WHERE world_id = ? AND id = ?
		`, worldID, id).Scan(
			&character.SceneID,
			&character.Floor,
			&character.Position.X,
			&character.Position.Y,
			&character.CurrentActivity,
			&character.Revision,
		)
		if err != nil {
			return fmt.Errorf("load meeting NPC %s: %w", id, err)
		}

		if character.SceneID != route.SceneID ||
			character.Floor != route.Floor {
			return fmt.Errorf("NPC %s is outside the meeting area", id)
		}
	}

	merchant := characters[0]
	blacksmith := characters[1]
	start := route.MerchantPoints[0]

	if math.Hypot(
		merchant.Position.X-start.X,
		merchant.Position.Y-start.Y,
	) > 2 {
		return fmt.Errorf("merchant is not at the route start")
	}

	if math.Hypot(
		blacksmith.Position.X-route.BlacksmithStand.X,
		blacksmith.Position.Y-route.BlacksmithStand.Y,
	) > 2 {
		return fmt.Errorf("blacksmith is not at the meeting stand")
	}

	// 使用游戏时间，不用现实时间决定移动进度。
	var rawGameTime string

	err = tx.QueryRowContext(ctx, `
		SELECT game_time FROM worlds WHERE id = ?
	`, worldID).Scan(&rawGameTime)
	if err != nil {
		return err
	}

	if _, err := time.Parse(time.RFC3339Nano, rawGameTime); err != nil {
		return fmt.Errorf("invalid world game time: %w", err)
	}

	for _, character := range characters {
		points := route.MerchantPoints
		jobStatus := "running"
		arrivalStatus := "travelling"
		activity := "walking_to_meeting"

		if character.ID == "blacksmith" {
			// 罗兰已经在站位，记录原地到场。
			// 保存这条记录也能保留他之前的活动。
			points = []model.Position{character.Position}
			jobStatus = "arrived"
			arrivalStatus = "arrived"
			activity = "waiting_for_meeting"
		} else {
			// 用实际起点替换近似起点，避免开始时发生小幅传送。
			points = append([]model.Position(nil), points...)
			points[0] = character.Position
		}

		rawRoute, err := json.Marshal(points)
		if err != nil {
			return err
		}

		result, err := tx.ExecContext(ctx, `
			UPDATE npcs
			SET current_activity = ?,
				revision = revision + 1
			WHERE world_id = ? AND id = ? AND revision = ?
		`,
			activity,
			worldID,
			character.ID,
			character.Revision,
		)
		if err != nil {
			return err
		}

		count, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if count != 1 {
			return fmt.Errorf("NPC %s changed during meeting setup", character.ID)
		}

		_, err = tx.ExecContext(ctx, `
			INSERT INTO npc_move_jobs (
				world_id, id, negotiation_id, npc_id,
				route_id, route_json, scene_id, floor,
				speed, started_game_time,
				previous_activity, expected_npc_revision,
				status, revision
			)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1)
		`,
			worldID,
			n.ID+":move:"+character.ID,
			n.ID,
			character.ID,
			route.ID,
			string(rawRoute),
			route.SceneID,
			route.Floor,
			route.Speed,
			rawGameTime,
			character.CurrentActivity,
			character.Revision+1,
			jobStatus,
		)
		if err != nil {
			return fmt.Errorf("create meeting job: %w", err)
		}

		result, err = tx.ExecContext(ctx, `
			UPDATE negotiation_participants
			SET arrival_status = ?
			WHERE world_id = ?
				AND negotiation_id = ?
				AND npc_id = ?
				AND invite_status = 'accepted'
				AND arrival_status = 'not_started'
		`,
			arrivalStatus,
			worldID,
			n.ID,
			character.ID,
		)
		if err != nil {
			return err
		}

		count, err = result.RowsAffected()
		if err != nil {
			return err
		}
		if count != 1 {
			return fmt.Errorf("meeting participant changed")
		}
	}

	result, err := tx.ExecContext(ctx, `
		UPDATE negotiations
		SET status = 'travelling',
			next_actor_id = NULL,
			revision = revision + 1
		WHERE world_id = ?
			AND id = ?
			AND status = 'inviting'
			AND revision = ?
	`, worldID, n.ID, n.Revision)
	if err != nil {
		return err
	}

	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return fmt.Errorf("negotiation changed during meeting setup")
	}

	// 罗兰的真实位置已经通过校验，因此可以记录到场。
	_, err = tx.ExecContext(ctx, `
		INSERT INTO negotiation_events (
			world_id, id, negotiation_id,
			actor_kind, actor_id, type,
			negotiation_revision, text, status
		)
		VALUES (?, ?, ?, 'system', 'game_service',
			'arrival', ?, ?, 'done')
	`,
		worldID,
		n.ID+":arrival:blacksmith",
		n.ID,
		n.Revision+1,
		"罗兰已经在铁匠铺会面点等候。",
	)
	if err != nil {
		return err
	}

	return tx.Commit()
}
