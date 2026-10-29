package game

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

type RepairCoordinationEvent struct {
	WorldID          string `json:"world_id"`
	ID               string `json:"id"`
	ProposalID       string `json:"proposal_id"`
	ProposalRevision int64  `json:"proposal_revision"`
	PlayerID         string `json:"player_id"`
	ActorNPCID       string `json:"actor_npc_id"`
	RecipientNPCID   string `json:"recipient_npc_id"`
	Decision         string `json:"decision"`
	Reason           string `json:"reason"`
	Status           string `json:"status"`
}

func RecordRepairResponse(
	ctx context.Context,
	db *sql.DB,
	worldID string,
	playerID string,
	actorNPCID string,
	turnID string,
	proposalID string,
	expectedRevision int64,
	decision string,
	reason string,
) (RepairCoordinationEvent, error) {
	reason = strings.TrimSpace(reason)

	if worldID == "" || playerID == "" || turnID == "" ||
		proposalID == "" || actorNPCID != "merchant" ||
		expectedRevision < 1 {
		return RepairCoordinationEvent{}, fmt.Errorf(
			"invalid repair response context",
		)
	}

	if decision != "reject" && decision != "request_revision" {
		return RepairCoordinationEvent{}, fmt.Errorf(
			"invalid repair response decision",
		)
	}

	if reason == "" || !utf8.ValidString(reason) ||
		utf8.RuneCountInString(reason) > 256 {
		return RepairCoordinationEvent{}, fmt.Errorf(
			"repair response reason must contain 1 to 256 characters",
		)
	}

	// 相同报价版本、角色和意见复用同一个事件。
	// 重复说相同内容，不重复调用导演。
	key, err := json.Marshal([]any{
		worldID, playerID, proposalID, expectedRevision,
		actorNPCID, decision, reason,
	})
	if err != nil {
		return RepairCoordinationEvent{}, err
	}

	sum := sha256.Sum256(key)

	event := RepairCoordinationEvent{
		WorldID:          worldID,
		ID:               fmt.Sprintf("repair-response-%x", sum[:16]),
		ProposalID:       proposalID,
		ProposalRevision: expectedRevision,
		PlayerID:         playerID,
		ActorNPCID:       actorNPCID,
		Decision:         decision,
		Reason:           reason,
		Status:           "pending",
	}

	// 明确拒绝并不自动触发继续劝说。
	if decision == "reject" {
		event.Status = "blocked"
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return RepairCoordinationEvent{}, err
	}
	defer tx.Rollback()

	// 当前开发预算：
	// 每份报价最多保存 3 次不同意见，间隔至少 30 秒。
	// 仅允许对已经收到、尚未接受的有效报价提出意见。
	result, err := tx.ExecContext(ctx, `
		INSERT INTO repair_coordination_events (
			world_id, id, proposal_id, proposal_revision,
			player_id, actor_npc_id, recipient_npc_id,
			source_turn_id, decision, reason, status
		)
		SELECT
			p.world_id, ?, p.id, p.revision,
			?, ?, p.worker_npc_id,
			?, ?, ?, ?
		FROM repair_proposals AS p
		WHERE p.world_id = ?
			AND p.id = ?
			AND p.revision = ?
			AND p.payer_npc_id = ?
			AND p.worker_confirmed = 1
			AND p.payer_confirmed = 0
			AND p.status = 'proposed'
			AND EXISTS (
				SELECT 1
				FROM repair_proposal_deliveries AS d
				WHERE d.world_id = p.world_id
					AND d.proposal_id = p.id
					AND d.player_id = ?
					AND d.recipient_npc_id = ?
					AND d.received_at IS NOT NULL
			)
			AND (
				SELECT COUNT(*)
				FROM repair_coordination_events AS e
				WHERE e.world_id = p.world_id
					AND e.proposal_id = p.id
			) < 3
			AND NOT EXISTS (
				SELECT 1
				FROM repair_coordination_events AS e
				WHERE e.world_id = p.world_id
					AND e.proposal_id = p.id
					AND e.created_at > unixepoch() - 30
			)
		ON CONFLICT (world_id, id) DO NOTHING
	`,
		event.ID,
		playerID, actorNPCID,
		turnID, decision, reason, event.Status,
		worldID, proposalID, expectedRevision, actorNPCID,
		playerID, actorNPCID,
	)
	if err != nil {
		return RepairCoordinationEvent{}, err
	}

	inserted, err := result.RowsAffected()
	if err != nil {
		return RepairCoordinationEvent{}, err
	}

	// 重复请求即使没有重新插入，也返回原事件。
	err = tx.QueryRowContext(ctx, `
		SELECT recipient_npc_id, status
		FROM repair_coordination_events
		WHERE world_id = ? AND id = ?
	`, worldID, event.ID).Scan(
		&event.RecipientNPCID,
		&event.Status,
	)
	if err == sql.ErrNoRows {
		return RepairCoordinationEvent{}, fmt.Errorf(
			"proposal unavailable, not received, or negotiation budget exceeded",
		)
	}
	if err != nil {
		return RepairCoordinationEvent{}, err
	}

	if inserted > 0 {
		// 新意见替代本人对同一报价的旧意见。
		// 已经在运行的旧导演结果也不能再落地。
		_, err = tx.ExecContext(ctx, `
			UPDATE repair_coordination_events
			SET status = 'superseded'
			WHERE world_id = ?
				AND proposal_id = ?
				AND actor_npc_id = ?
				AND id != ?
				AND status IN (
					'pending', 'processing', 'delivered', 'waiting'
				)
		`, worldID, proposalID, actorNPCID, event.ID)
		if err != nil {
			return RepairCoordinationEvent{}, err
		}
	}

	if err := tx.Commit(); err != nil {
		return RepairCoordinationEvent{}, err
	}

	return event, nil
}

func ClaimRepairCoordination(
	ctx context.Context,
	db *sql.DB,
) (RepairCoordinationEvent, error) {
	var event RepairCoordinationEvent

	err := db.QueryRowContext(ctx, `
		SELECT
			world_id, id, proposal_id, proposal_revision,
			player_id, actor_npc_id, recipient_npc_id,
			decision, reason, status
		FROM repair_coordination_events
		WHERE status = 'pending' AND attempts = 0
		ORDER BY created_at, rowid
		LIMIT 1
	`).Scan(
		&event.WorldID,
		&event.ID,
		&event.ProposalID,
		&event.ProposalRevision,
		&event.PlayerID,
		&event.ActorNPCID,
		&event.RecipientNPCID,
		&event.Decision,
		&event.Reason,
		&event.Status,
	)
	if err != nil {
		return RepairCoordinationEvent{}, err
	}

	result, err := db.ExecContext(ctx, `
		UPDATE repair_coordination_events
		SET status = 'processing', attempts = attempts + 1
		WHERE world_id = ?
			AND id = ?
			AND status = 'pending'
			AND attempts = 0
	`, event.WorldID, event.ID)
	if err != nil {
		return RepairCoordinationEvent{}, err
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return RepairCoordinationEvent{}, err
	}
	if affected != 1 {
		return RepairCoordinationEvent{}, sql.ErrNoRows
	}

	event.Status = "processing"
	return event, nil
}

func FinishRepairCoordination(
	ctx context.Context,
	db *sql.DB,
	event RepairCoordinationEvent,
	action string,
	reason string,
) (string, error) {
	var nextStatus string

	switch action {
	case "deliver_revision_request":
		nextStatus = "delivered"
	case "wait":
		nextStatus = "waiting"
	case "failed":
		nextStatus = "failed"
	default:
		return "", fmt.Errorf("unsupported coordination action")
	}

	if reason == "" || !utf8.ValidString(reason) ||
		utf8.RuneCountInString(reason) > 256 {
		return "", fmt.Errorf("invalid coordination reason")
	}

	// 记录导演结果与消息送达在同一条 UPDATE 中完成。
	// 报价失效时只能记为 stale，不能送达旧请求。
	_, err := db.ExecContext(ctx, `
		UPDATE repair_coordination_events
		SET
			status = CASE WHEN EXISTS (
				SELECT 1
				FROM repair_proposals AS p
				WHERE p.world_id =
					repair_coordination_events.world_id
					AND p.id =
						repair_coordination_events.proposal_id
					AND p.revision =
						repair_coordination_events.proposal_revision
					AND p.payer_npc_id =
						repair_coordination_events.actor_npc_id
					AND p.worker_npc_id =
						repair_coordination_events.recipient_npc_id
					AND p.status = 'proposed'
					AND p.worker_confirmed = 1
					AND p.payer_confirmed = 0
			) THEN ? ELSE 'stale' END,
			plan_action = ?,
			plan_reason = ?
		WHERE world_id = ?
			AND id = ?
			AND status = 'processing'
			AND decision = 'request_revision'
	`,
		nextStatus, action, reason,
		event.WorldID, event.ID,
	)
	if err != nil {
		return "", err
	}

	var status string
	err = db.QueryRowContext(ctx, `
		SELECT status
		FROM repair_coordination_events
		WHERE world_id = ? AND id = ?
	`, event.WorldID, event.ID).Scan(&status)

	return status, err
}

// 当前约定只有一个游戏服务进程。
// 上次进程中断的调用标记失败，不自动重复花费模型额度。
func RecoverRepairCoordination(
	ctx context.Context,
	db *sql.DB,
) error {
	_, err := db.ExecContext(ctx, `
		UPDATE repair_coordination_events
		SET status = 'failed',
			plan_reason = '服务进程中断，本次协调未完成'
		WHERE status = 'processing'
	`)
	return err
}

func GetRepairCoordinationFacts(
	ctx context.Context,
	db *sql.DB,
	worldID string,
	npcID string,
) ([]KnownFact, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT e.id, e.proposal_id, e.reason, n.name
		FROM repair_coordination_events AS e
		JOIN repair_proposals AS p
			ON p.world_id = e.world_id
			AND p.id = e.proposal_id
		JOIN npcs AS n
			ON n.world_id = e.world_id
			AND n.id = e.actor_npc_id
		WHERE e.world_id = ?
			AND e.recipient_npc_id = ?
			AND e.status = 'delivered'
			AND p.revision = e.proposal_revision
			AND p.status = 'proposed'
			AND p.payer_confirmed = 0
		ORDER BY e.created_at DESC, e.rowid DESC
		LIMIT 3
	`, worldID, npcID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	facts := make([]KnownFact, 0)

	for rows.Next() {
		var id, proposalID, reason, senderName string

		if err := rows.Scan(
			&id,
			&proposalID,
			&reason,
			&senderName,
		); err != nil {
			return nil, err
		}

		facts = append(facts, KnownFact{
			ID: "repair-request:" + id,
			Content: fmt.Sprintf(
				"%s针对维修报价%s提出修改请求：%s。"+
					"这只是对方的要求，不代表你已经同意，"+
					"也没有修改报价或启动维修。",
				senderName,
				proposalID,
				reason,
			),
			Source: senderName + "通过已经送达的维修交涉消息告知。",
		})
	}

	return facts, rows.Err()
}
