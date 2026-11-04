package negotiation

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"

	"cybertown/internal/game/model"
	"cybertown/internal/game/repair"
)

func discussionID(source, suffix string) string {
	sum := sha256.Sum256([]byte(source + "/" + suffix))
	return fmt.Sprintf("discussion-%x", sum[:16])
}

func discussionReadyTx(
	ctx context.Context,
	tx *sql.Tx,
	n Negotiation,
) (repair.RepairProposal, error) {
	if n.Status != "discussing" {
		return repair.RepairProposal{}, ErrDiscussionState
	}

	p, err := repair.GetRepairProposal(
		ctx, tx, n.WorldID, n.CurrentProposalID,
	)
	if err != nil {
		return p, err
	}

	if p.Revision != n.CurrentProposalRevision ||
		p.CartID != n.CartID ||
		p.PayerNPCID != "merchant" ||
		p.WorkerNPCID != "blacksmith" ||
		p.Status != "proposed" ||
		p.PayerConfirmed ||
		!p.WorkerConfirmed {
		return p, ErrDiscussionState
	}

	var usable bool
	err = tx.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM carts c
			JOIN players pl ON pl.world_id = c.world_id
			WHERE c.world_id = ? AND c.id = ?
				AND c.repair_status = 'damaged'
				AND pl.id = ? AND pl.status = 'active'
		)
		AND NOT EXISTS (
			SELECT 1 FROM repair_jobs
			WHERE world_id = ? AND cart_id = ? AND status = 'running'
		)
		AND NOT EXISTS (
			SELECT 1 FROM transport_jobs
			WHERE world_id = ? AND cart_id = ? AND status = 'in_transit'
		)
	`, n.WorldID, n.CartID, n.PlayerID,
		n.WorldID, n.CartID, n.WorldID, n.CartID,
	).Scan(&usable)
	if err != nil {
		return p, err
	}
	if !usable {
		return p, ErrDiscussionState
	}

	rows, err := tx.QueryContext(ctx, `
		SELECT
			part.npc_id, part.invite_status, part.arrival_status,
			j.status, j.scene_id, j.floor, j.route_json,
			j.expected_npc_revision,
			a.scene_id, a.floor, a.position_x, a.position_y,
			a.current_activity, a.revision
		FROM negotiation_participants part
		JOIN npc_move_jobs j
			ON j.world_id = part.world_id
			AND j.negotiation_id = part.negotiation_id
			AND j.npc_id = part.npc_id
		JOIN npcs a
			ON a.world_id = part.world_id AND a.id = part.npc_id
		WHERE part.world_id = ? AND part.negotiation_id = ?
	`, n.WorldID, n.ID)
	if err != nil {
		return p, err
	}
	defer rows.Close()

	count := 0
	for rows.Next() {
		var id, invite, arrival, jobStatus, routeScene, scene, routeJSON, activity string
		var routeFloor, floor int
		var expected, revision int64
		var x, y float64

		if err := rows.Scan(
			&id, &invite, &arrival,
			&jobStatus, &routeScene, &routeFloor, &routeJSON, &expected,
			&scene, &floor, &x, &y, &activity, &revision,
		); err != nil {
			return p, err
		}

		var points []model.Position
		if err := json.Unmarshal([]byte(routeJSON), &points); err != nil {
			return p, err
		}
		if len(points) == 0 {
			return p, ErrDiscussionState
		}

		target := points[len(points)-1]
		distance := math.Hypot(x-target.X, y-target.Y)

		if (id != "merchant" && id != "blacksmith") ||
			invite != "accepted" ||
			arrival != "arrived" ||
			jobStatus != "arrived" ||
			scene != routeScene ||
			floor != routeFloor ||
			activity != "attending_meeting" ||
			expected != revision ||
			math.IsNaN(distance) ||
			math.IsInf(distance, 0) ||
			distance > 2 {
			return p, ErrDiscussionState
		}
		count++
	}
	if err := rows.Err(); err != nil {
		return p, err
	}
	if count != 2 {
		return p, ErrDiscussionState
	}

	return p, nil
}

func discussionSelfTx(
	ctx context.Context,
	tx *sql.Tx,
	worldID, npcID string,
) (DiscussionSelf, error) {
	var self DiscussionSelf

	err := tx.QueryRowContext(ctx, `
		SELECT name, persona, background, revision,
			COALESCE((
				SELECT quantity FROM npc_inventory
				WHERE world_id = npcs.world_id
					AND npc_id = npcs.id AND item_id = 'coin'
			), 0),
			COALESCE((
				SELECT revision FROM npc_inventory
				WHERE world_id = npcs.world_id
					AND npc_id = npcs.id AND item_id = 'coin'
			), 0),
			COALESCE((
				SELECT quantity FROM npc_inventory
				WHERE world_id = npcs.world_id
					AND npc_id = npcs.id AND item_id = 'repair_material'
			), 0),
			COALESCE((
				SELECT revision FROM npc_inventory
				WHERE world_id = npcs.world_id
					AND npc_id = npcs.id AND item_id = 'repair_material'
			), 0)
		FROM npcs
		WHERE world_id = ? AND id = ?
	`, worldID, npcID).Scan(
		&self.Name, &self.Persona, &self.Background, &self.Revision,
		&self.Coins, &self.CoinsRevision,
		&self.Materials, &self.MaterialRevision,
	)
	return self, err
}

func insertDiscussionEventTx(
	ctx context.Context,
	tx *sql.Tx,
	n Negotiation,
	id, actor, recipient, eventType, source, text, status, kind, topic string,
	public bool,
	payload discussionPayload,
) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	actorKind := "npc"
	switch actor {
	case "game_service":
		actorKind = "system"
	case "town-director":
		actorKind = "director"
	case n.PlayerID:
		actorKind = "player"
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO negotiation_events (
			world_id, id, negotiation_id,
			actor_kind, actor_id, recipient_id, type,
			proposal_id, proposal_revision, source_event_id,
			negotiation_revision, text, status
		)
		VALUES (?, ?, ?, ?, ?, NULLIF(?, ''), ?,
			?, ?, NULLIF(?, ''), ?, ?, ?)
	`,
		n.WorldID, id, n.ID, actorKind, actor, recipient, eventType,
		n.CurrentProposalID, n.CurrentProposalRevision, source,
		n.Revision, text, status,
	)
	if err != nil {
		return err
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO negotiation_discussion_data (
			world_id, event_id, kind, topic, payload_json, public
		)
		VALUES (?, ?, ?, ?, ?, ?)
	`, n.WorldID, id, kind, topic, string(raw), public)
	return err
}

func queueDiscussionTx(
	ctx context.Context,
	tx *sql.Tx,
	n Negotiation,
	source, actor, kind, topic string,
) error {
	id := discussionID(source, "work")
	eventType := "proposal_response"
	if kind == DiscussionDirector {
		eventType = "director_request"
		actor = ""
	}

	if err := insertDiscussionEventTx(
		ctx, tx, n, id, "game_service", actor,
		eventType, source, "", "pending", kind, topic,
		false, discussionPayload{},
	); err != nil {
		return err
	}

	_, err := tx.ExecContext(ctx, `
		UPDATE negotiations
		SET next_actor_id = NULLIF(?, '')
		WHERE world_id = ? AND id = ?
	`, actor, n.WorldID, n.ID)
	return err
}

func deliverDiscussionProposalTx(
	ctx context.Context,
	tx *sql.Tx,
	n Negotiation,
	source string,
) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO repair_proposal_deliveries (
			world_id, proposal_id, player_id,
			recipient_npc_id, source_turn_id, received_at
		)
		VALUES (?, ?, ?, 'merchant', ?, CURRENT_TIMESTAMP)
		ON CONFLICT (
			world_id, proposal_id, player_id, recipient_npc_id
		)
		DO UPDATE SET
			received_at = COALESCE(
				repair_proposal_deliveries.received_at,
				excluded.received_at
			)
	`, n.WorldID, n.CurrentProposalID, n.PlayerID, source)
	return err
}

func discussionHistoryTx(
	ctx context.Context,
	tx *sql.Tx,
	worldID, negotiationID string,
) ([]DiscussionLine, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT e.seq, e.actor_id, e.text, d.payload_json
		FROM negotiation_events e
		JOIN negotiation_discussion_data d
			ON d.world_id = e.world_id AND d.event_id = e.id
		WHERE e.world_id = ? AND e.negotiation_id = ?
			AND e.status = 'done' AND d.public = 1
		ORDER BY e.seq
	`, worldID, negotiationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	lines := make([]DiscussionLine, 0)
	for rows.Next() {
		var line DiscussionLine
		var raw string
		if err := rows.Scan(
			&line.Seq, &line.Actor, &line.Speech, &raw,
		); err != nil {
			return nil, err
		}

		var payload discussionPayload
		if err := json.Unmarshal([]byte(raw), &payload); err != nil {
			return nil, err
		}
		if payload.Decision != nil {
			line.Action = payload.Decision.Action
			line.Topic = payload.Decision.Topic
			line.Reason = payload.Decision.Reason
		}
		lines = append(lines, line)
	}
	return lines, rows.Err()
}

func endDiscussionTx(
	ctx context.Context,
	tx *sql.Tx,
	n Negotiation,
	status, reason string,
) error {
	if n.Status != "inviting" &&
		n.Status != "travelling" &&
		n.Status != "discussing" {
		return nil
	}

	_, err := tx.ExecContext(ctx, `
		UPDATE negotiations
		SET status = ?, end_reason = ?, next_actor_id = NULL,
			revision = revision + 1
		WHERE world_id = ? AND id = ?
	`, status, reason, n.WorldID, n.ID)
	if err != nil {
		return err
	}

	_, err = tx.ExecContext(ctx, `
		UPDATE negotiation_events
		SET status = 'cancelled'
		WHERE world_id = ? AND negotiation_id = ?
			AND status IN ('pending', 'processing')
	`, n.WorldID, n.ID)
	if err != nil {
		return err
	}

	// 会面结束后留在当前位置。
	// 不在这里假装已经走回商店，也不覆盖其他系统的新活动。
	_, err = tx.ExecContext(ctx, `
		UPDATE npcs
		SET current_activity = 'idle', revision = revision + 1
		WHERE world_id = ?
			AND current_activity IN (
				'walking_to_meeting',
				'waiting_for_meeting',
				'attending_meeting'
			)
			AND EXISTS (
				SELECT 1 FROM npc_move_jobs j
				WHERE j.world_id = npcs.world_id
					AND j.npc_id = npcs.id
					AND j.negotiation_id = ?
					AND j.expected_npc_revision = npcs.revision
			)
	`, n.WorldID, n.ID)
	if err != nil {
		return err
	}

	_, err = tx.ExecContext(ctx, `
		UPDATE npc_move_jobs
		SET status = 'cancelled', revision = revision + 1
		WHERE world_id = ? AND negotiation_id = ? AND status = 'running'
	`, n.WorldID, n.ID)
	if err != nil {
		return err
	}

	n.Revision++
	if err := insertDiscussionEventTx(
		ctx, tx, n, discussionID(n.ID, "ended"),
		"game_service", "", "ended", "",
		reason, "done", "result", "", true, discussionPayload{},
	); err != nil {
		return err
	}

	_, err = tx.ExecContext(ctx, `
		UPDATE worlds SET revision = revision + 1 WHERE id = ?
	`, n.WorldID)
	return err
}

func EnsureDiscussionStarted(
	ctx context.Context,
	db *sql.DB,
	worldID, negotiationID string,
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
	if n.Status != "discussing" {
		return nil
	}

	openingID := discussionID(n.ID, "opening")
	var exists bool
	err = tx.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM negotiation_events
			WHERE world_id = ? AND id = ?
		)
	`, worldID, openingID).Scan(&exists)
	if err != nil || exists {
		return err
	}

	p, err := discussionReadyTx(ctx, tx, n)
	if errors.Is(err, ErrDiscussionState) {
		if err := endDiscussionTx(
			ctx, tx, n, "blocked", "会面条件或当前报价已经变化。",
		); err != nil {
			return err
		}
		return tx.Commit()
	}
	if err != nil {
		return err
	}

	text := fmt.Sprintf(
		"罗兰向马丁出示维修报价：工钱%d枚钱币，罗兰提供%d份材料，工时%d分钟。",
		p.PriceCoins, p.MaterialCount, p.DurationMinutes,
	)

	if err := insertDiscussionEventTx(
		ctx, tx, n, openingID, "game_service", "",
		"proposal", "", text, "done", "opening", "",
		true, discussionPayload{},
	); err != nil {
		return err
	}
	if err := deliverDiscussionProposalTx(ctx, tx, n, openingID); err != nil {
		return err
	}
	if err := queueDiscussionTx(
		ctx, tx, n, openingID, "merchant", DiscussionReview, "",
	); err != nil {
		return err
	}

	_, err = tx.ExecContext(ctx, `
		UPDATE negotiations SET revision = revision + 1
		WHERE world_id = ? AND id = ?
	`, worldID, n.ID)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func EnsureAllDiscussions(ctx context.Context, db *sql.DB) error {
	rows, err := db.QueryContext(ctx, `
		SELECT world_id, id FROM negotiations WHERE status = 'discussing'
	`)
	if err != nil {
		return err
	}

	var ids [][2]string
	for rows.Next() {
		var pair [2]string
		if err := rows.Scan(&pair[0], &pair[1]); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, pair)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}

	for _, pair := range ids {
		if err := EnsureDiscussionStarted(ctx, db, pair[0], pair[1]); err != nil {
			return err
		}
	}
	return nil
}
