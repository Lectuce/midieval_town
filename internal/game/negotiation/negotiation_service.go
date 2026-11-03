package negotiation

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

var (
	ErrNegotiationInvalidRequest = errors.New(
		"invalid negotiation request",
	)
	ErrNegotiationRequestConflict = errors.New(
		"negotiation request conflicts with previous request",
	)
	ErrNegotiationUnavailable = errors.New(
		"proposal unavailable for negotiation",
	)
	ErrNegotiationBusy = errors.New(
		"cart or participant is already occupied",
	)
)

type CreateNegotiationRequest struct {
	RequestID        string
	ProposalID       string
	ExpectedRevision int64
}

func CreateNegotiation(
	ctx context.Context,
	db *sql.DB,
	worldID string,
	playerID string,
	req CreateNegotiationRequest,
) (Negotiation, error) {
	if db == nil {
		return Negotiation{}, fmt.Errorf("database is required")
	}

	if strings.TrimSpace(worldID) == "" ||
		strings.TrimSpace(playerID) == "" ||
		strings.TrimSpace(req.RequestID) == "" ||
		len(req.RequestID) > 128 ||
		strings.TrimSpace(req.ProposalID) == "" ||
		len(req.ProposalID) > 128 ||
		req.ExpectedRevision < 1 {
		return Negotiation{}, ErrNegotiationInvalidRequest
	}

	// 同一玩家的同一请求始终得到同一个会话 ID。
	key := fmt.Sprintf(
		"%q/%q/%q",
		worldID,
		playerID,
		req.RequestID,
	)
	sum := sha256.Sum256([]byte(key))
	negotiationID := fmt.Sprintf("negotiation-%x", sum[:16])

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return Negotiation{}, fmt.Errorf(
			"begin negotiation: %w", err,
		)
	}
	defer tx.Rollback()

	// 先处理重复请求。
	// 即使原会话已经结束，也不能因重试再创建一场。
	existing, err := getNegotiationTx(
		ctx, tx, worldID, negotiationID,
	)
	if err == nil {
		if existing.PlayerID != playerID ||
			existing.RequestID != req.RequestID ||
			existing.InitialProposalID != req.ProposalID ||
			existing.InitialProposalRevision != req.ExpectedRevision {
			return Negotiation{}, ErrNegotiationRequestConflict
		}

		return existing, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Negotiation{}, fmt.Errorf(
			"load previous negotiation: %w", err,
		)
	}

	// 参与者和货车必须从实际报价读取，
	// 不接受客户端指定“谁是付款人、谁负责维修”。
	var cartID string
	var payerNPCID string
	var workerNPCID string
	var proposalStatus string
	var proposalRevision int64
	var payerConfirmed bool
	var workerConfirmed bool

	err = tx.QueryRowContext(ctx, `
		SELECT
			cart_id,
			payer_npc_id,
			worker_npc_id,
			status,
			revision,
			payer_confirmed,
			worker_confirmed
		FROM repair_proposals
		WHERE world_id = ? AND id = ?
	`,
		worldID,
		req.ProposalID,
	).Scan(
		&cartID,
		&payerNPCID,
		&workerNPCID,
		&proposalStatus,
		&proposalRevision,
		&payerConfirmed,
		&workerConfirmed,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Negotiation{}, ErrNegotiationUnavailable
	}
	if err != nil {
		return Negotiation{}, fmt.Errorf(
			"load negotiation proposal: %w", err,
		)
	}

	// 当前第一版只支持马丁与罗兰的维修协商。
	if payerNPCID != "merchant" ||
		workerNPCID != "blacksmith" {
		return Negotiation{}, ErrNegotiationUnavailable
	}

	// 玩家必须有效，并且确实取得过这份报价。
	// 不要求 received_at 非空：
	// 玩家可以拿着尚未转交的报价发起邀请。
	var playerCanInvite bool

	err = tx.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM players AS pl
			JOIN repair_proposal_deliveries AS d
				ON d.world_id = pl.world_id
				AND d.player_id = pl.id
			WHERE pl.world_id = ?
				AND pl.id = ?
				AND pl.status = 'active'
				AND d.proposal_id = ?
				AND d.recipient_npc_id = ?
		)
	`,
		worldID,
		playerID,
		req.ProposalID,
		payerNPCID,
	).Scan(&playerCanInvite)
	if err != nil {
		return Negotiation{}, fmt.Errorf(
			"check negotiation permission: %w", err,
		)
	}
	if !playerCanInvite {
		return Negotiation{}, ErrNegotiationUnavailable
	}

	// 不同按钮请求再次邀请同一份正在协商的报价，
	// 返回已有会话，不额外创建邀请。
	var activeID string

	err = tx.QueryRowContext(ctx, `
		SELECT id
		FROM negotiations
		WHERE world_id = ?
			AND cart_id = ?
			AND status IN (
				'inviting', 'travelling', 'discussing'
			)
	`,
		worldID,
		cartID,
	).Scan(&activeID)

	if err == nil {
		active, err := getNegotiationTx(
			ctx, tx, worldID, activeID,
		)
		if err != nil {
			return Negotiation{}, fmt.Errorf(
				"load active negotiation: %w", err,
			)
		}

		if active.PlayerID == playerID &&
			active.CurrentProposalID == req.ProposalID &&
			active.CurrentProposalRevision == req.ExpectedRevision {
			return active, nil
		}

		return Negotiation{}, ErrNegotiationBusy
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Negotiation{}, fmt.Errorf(
			"check active negotiation: %w", err,
		)
	}

	if proposalRevision != req.ExpectedRevision {
		return Negotiation{}, ErrRepairProposalRevisionConflict
	}

	// 已经双方同意的报价无须再次发起报价协商。
	if proposalStatus != RepairProposalStatusProposed ||
		payerConfirmed ||
		!workerConfirmed {
		return Negotiation{}, ErrNegotiationUnavailable
	}

	var cartNeedsRepair bool

	err = tx.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM carts
			WHERE world_id = ?
				AND id = ?
				AND repair_status = 'damaged'
		)
	`,
		worldID,
		cartID,
	).Scan(&cartNeedsRepair)
	if err != nil {
		return Negotiation{}, fmt.Errorf(
			"check negotiation cart: %w", err,
		)
	}
	if !cartNeedsRepair {
		return Negotiation{}, ErrNegotiationUnavailable
	}

	// 检查当前已实现的占用：
	// 其他活动协商、正在维修、货车正在运输，
	// 以及同一货车已经成立的维修协议。
	var occupied bool

	err = tx.QueryRowContext(ctx, `
		SELECT
			EXISTS (
				SELECT 1
				FROM negotiation_participants AS np
				JOIN negotiations AS n
					ON n.world_id = np.world_id
					AND n.id = np.negotiation_id
				WHERE np.world_id = ?
					AND np.npc_id IN (?, ?)
					AND n.status IN (
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
						OR worker_npc_id IN (?, ?)
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
		worldID, payerNPCID, workerNPCID,
		worldID, cartID, payerNPCID, workerNPCID,
		worldID, cartID,
		worldID, cartID,
	).Scan(&occupied)
	if err != nil {
		return Negotiation{}, fmt.Errorf(
			"check negotiation availability: %w", err,
		)
	}
	if occupied {
		return Negotiation{}, ErrNegotiationBusy
	}

	negotiation := Negotiation{
		WorldID:                 worldID,
		ID:                      negotiationID,
		PlayerID:                playerID,
		CartID:                  cartID,
		RequestID:               req.RequestID,
		InitialProposalID:       req.ProposalID,
		InitialProposalRevision: req.ExpectedRevision,
		CurrentProposalID:       req.ProposalID,
		CurrentProposalRevision: req.ExpectedRevision,
		Status:                  NegotiationStatusInviting,
		Revision:                1,
		CreatedAt:               time.Now().Unix(),
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO negotiations (
			world_id,
			id,
			player_id,
			cart_id,
			request_id,
			initial_proposal_id,
			initial_proposal_revision,
			current_proposal_id,
			current_proposal_revision,
			status,
			revision,
			created_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		negotiation.WorldID,
		negotiation.ID,
		negotiation.PlayerID,
		negotiation.CartID,
		negotiation.RequestID,
		negotiation.InitialProposalID,
		negotiation.InitialProposalRevision,
		negotiation.CurrentProposalID,
		negotiation.CurrentProposalRevision,
		negotiation.Status,
		negotiation.Revision,
		negotiation.CreatedAt,
	)
	if err != nil {
		return Negotiation{}, fmt.Errorf(
			"create negotiation: %w", err,
		)
	}

	for _, npcID := range []string{payerNPCID, workerNPCID} {
		_, err = tx.ExecContext(ctx, `
			INSERT INTO negotiation_participants (
				world_id,
				negotiation_id,
				npc_id,
				invite_status,
				arrival_status
			)
			VALUES (?, ?, ?, 'pending', 'not_started')
		`,
			worldID,
			negotiation.ID,
			npcID,
		)
		if err != nil {
			return Negotiation{}, fmt.Errorf(
				"create negotiation participant: %w", err,
			)
		}

		// 一名参与者对应一条独立邀请，
		// 后续后台任务分别处理。
		eventID := negotiation.ID + ":invite:" + npcID

		_, err = tx.ExecContext(ctx, `
			INSERT INTO negotiation_events (
				world_id,
				id,
				negotiation_id,
				actor_kind,
				actor_id,
				recipient_id,
				type,
				proposal_id,
				proposal_revision,
				negotiation_revision,
				text,
				status,
				created_at
			)
			VALUES (
				?, ?, ?,
				'player', ?, ?,
				'invitation', ?, ?, ?,
				?, 'pending', ?
			)
		`,
			worldID,
			eventID,
			negotiation.ID,
			playerID,
			npcID,
			req.ProposalID,
			req.ExpectedRevision,
			negotiation.Revision,
			"玩家邀请你与另一位报价参与者当面商量货车维修。",
			negotiation.CreatedAt,
		)
		if err != nil {
			return Negotiation{}, fmt.Errorf(
				"create negotiation invitation: %w", err,
			)
		}
	}

	if err := tx.Commit(); err != nil {
		return Negotiation{}, fmt.Errorf(
			"commit negotiation: %w", err,
		)
	}

	return negotiation, nil
}

// 只在当前事务中读取，避免重新申请数据库连接。
func getNegotiationTx(
	ctx context.Context,
	tx *sql.Tx,
	worldID string,
	id string,
) (Negotiation, error) {
	var n Negotiation

	err := tx.QueryRowContext(ctx, `
		SELECT
			world_id,
			id,
			player_id,
			cart_id,
			request_id,
			initial_proposal_id,
			initial_proposal_revision,
			current_proposal_id,
			current_proposal_revision,
			status,
			COALESCE(next_actor_id, ''),
			round_count,
			director_calls,
			revision,
			created_at
		FROM negotiations
		WHERE world_id = ? AND id = ?
	`,
		worldID,
		id,
	).Scan(
		&n.WorldID,
		&n.ID,
		&n.PlayerID,
		&n.CartID,
		&n.RequestID,
		&n.InitialProposalID,
		&n.InitialProposalRevision,
		&n.CurrentProposalID,
		&n.CurrentProposalRevision,
		&n.Status,
		&n.NextActorID,
		&n.RoundCount,
		&n.DirectorCalls,
		&n.Revision,
		&n.CreatedAt,
	)

	return n, err
}

func ClaimNegotiationInvitation(
	ctx context.Context,
	db *sql.DB,
) (NegotiationInvitation, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return NegotiationInvitation{}, err
	}
	defer tx.Rollback()

	var job NegotiationInvitation
	var worldID, negotiationID string

	// 同一会话中，有一个事件正在处理时，不领取另一条。
	err = tx.QueryRowContext(ctx, `
		SELECT
			e.world_id,
			e.negotiation_id,
			e.id,
			e.recipient_id,
			e.text
		FROM negotiation_events AS e
		JOIN negotiations AS n
			ON n.world_id = e.world_id
			AND n.id = e.negotiation_id
		JOIN negotiation_participants AS p
			ON p.world_id = e.world_id
			AND p.negotiation_id = e.negotiation_id
			AND p.npc_id = e.recipient_id
		WHERE e.type = 'invitation'
			AND e.status = 'pending'
			AND e.attempts = 0
			AND e.actor_kind = 'player'
			AND e.actor_id = n.player_id
			AND n.status = 'inviting'
			AND p.invite_status = 'pending'
			AND NOT EXISTS (
				SELECT 1
				FROM negotiation_events AS running
				WHERE running.world_id = e.world_id
					AND running.negotiation_id = e.negotiation_id
					AND running.status = 'processing'
			)
		ORDER BY e.seq
		LIMIT 1
	`).Scan(
		&worldID,
		&negotiationID,
		&job.EventID,
		&job.NPCID,
		&job.Text,
	)
	if err != nil {
		return NegotiationInvitation{}, err
	}

	job.Negotiation, err = getNegotiationTx(
		ctx, tx, worldID, negotiationID,
	)
	if err != nil {
		return NegotiationInvitation{}, err
	}

	result, err := tx.ExecContext(ctx, `
		UPDATE negotiation_events
		SET status = 'processing',
			attempts = attempts + 1,
			negotiation_revision = ?
		WHERE world_id = ?
			AND id = ?
			AND status = 'pending'
			AND attempts = 0
	`,
		job.Negotiation.Revision,
		worldID,
		job.EventID,
	)
	if err != nil {
		return NegotiationInvitation{}, err
	}

	count, err := result.RowsAffected()
	if err != nil {
		return NegotiationInvitation{}, err
	}
	if count != 1 {
		return NegotiationInvitation{}, fmt.Errorf(
			"invitation was already claimed",
		)
	}

	if err := tx.Commit(); err != nil {
		return NegotiationInvitation{}, err
	}

	return job, nil
}

func FinishNegotiationInvitation(
	ctx context.Context,
	db *sql.DB,
	job NegotiationInvitation,
	decision InvitationDecision,
) error {
	decision.Speech = strings.TrimSpace(decision.Speech)

	switch decision.Action {
	case "accept_invitation", "decline_invitation", "failed":
	default:
		return fmt.Errorf("invalid invitation decision")
	}

	if decision.Speech == "" ||
		!utf8.ValidString(decision.Speech) ||
		utf8.RuneCountInString(decision.Speech) > 256 {
		return fmt.Errorf("invalid invitation speech")
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	worldID := job.Negotiation.WorldID

	var eventStatus, recipientID, negotiationID string
	var claimedRevision int64

	err = tx.QueryRowContext(ctx, `
		SELECT
			status,
			recipient_id,
			negotiation_id,
			negotiation_revision
		FROM negotiation_events
		WHERE world_id = ?
			AND id = ?
			AND type = 'invitation'
	`,
		worldID,
		job.EventID,
	).Scan(
		&eventStatus,
		&recipientID,
		&negotiationID,
		&claimedRevision,
	)
	if err != nil {
		return err
	}

	if recipientID != job.NPCID ||
		negotiationID != job.Negotiation.ID {
		return fmt.Errorf("invitation identity mismatch")
	}

	// 已经处理过，不能重复写入回应。
	if eventStatus == "done" ||
		eventStatus == "failed" ||
		eventStatus == "cancelled" {
		return nil
	}
	if eventStatus != "processing" {
		return fmt.Errorf("invitation is not processing")
	}

	n, err := getNegotiationTx(ctx, tx, worldID, negotiationID)
	if err != nil {
		return err
	}

	// 会话已经结束，迟到的模型结果不能重新打开会话。
	if n.Status != NegotiationStatusInviting {
		_, err = tx.ExecContext(ctx, `
			UPDATE negotiation_events
			SET status = 'cancelled'
			WHERE world_id = ? AND id = ?
		`, worldID, job.EventID)
		if err != nil {
			return err
		}
		return tx.Commit()
	}

	// 保存正常决定前，再检查模型依据是否已经变化。
	if decision.Action != "failed" {
		var valid bool

		err = tx.QueryRowContext(ctx, `
			SELECT EXISTS (
				SELECT 1
				FROM repair_proposals AS p
				JOIN carts AS c
					ON c.world_id = p.world_id
					AND c.id = p.cart_id
				JOIN npcs AS character
					ON character.world_id = p.world_id
					AND character.id = ?
				WHERE p.world_id = ?
					AND p.id = ?
					AND p.revision = ?
					AND p.cart_id = ?
					AND p.status = 'proposed'
					AND p.worker_confirmed = 1
					AND p.payer_confirmed = 0
					AND c.repair_status = 'damaged'
					AND character.revision = ?
					AND character.id IN (
						p.payer_npc_id, p.worker_npc_id
					)
			)
		`,
			job.NPCID,
			worldID,
			n.CurrentProposalID,
			n.CurrentProposalRevision,
			n.CartID,
			job.NPCRevision,
		).Scan(&valid)
		if err != nil {
			return err
		}

		if !valid ||
			n.Revision != claimedRevision ||
			n.Revision != job.Negotiation.Revision {
			decision = InvitationDecision{
				Action: "failed",
				Speech: "邀请处理期间相关状态发生变化，本次处理已停止。",
			}
		}
	}

	nextStatus := NegotiationStatusInviting
	endReason := ""
	eventType := "invitation_response"
	actorKind := "npc"
	actorID := job.NPCID
	processedStatus := "done"

	if decision.Action == "failed" {
		nextStatus = NegotiationStatusFailed
		endReason = decision.Speech
		eventType = "ended"
		actorKind = "system"
		actorID = "negotiation_worker"
		processedStatus = "failed"
	} else {
		inviteStatus := "accepted"

		if decision.Action == "decline_invitation" {
			inviteStatus = "declined"
			nextStatus = NegotiationStatusBlocked
			endReason = "一位参与者拒绝参加本次会面。"
		}

		result, err := tx.ExecContext(ctx, `
			UPDATE negotiation_participants
			SET invite_status = ?
			WHERE world_id = ?
				AND negotiation_id = ?
				AND npc_id = ?
				AND invite_status = 'pending'
				AND arrival_status = 'not_started'
		`,
			inviteStatus,
			worldID,
			n.ID,
			job.NPCID,
		)
		if err != nil {
			return err
		}

		count, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if count != 1 {
			return fmt.Errorf("participant state changed")
		}
	}

	_, err = tx.ExecContext(ctx, `
		UPDATE negotiations
		SET status = ?,
			end_reason = ?,
			next_actor_id = NULL,
			revision = revision + 1
		WHERE world_id = ? AND id = ?
	`, nextStatus, endReason, worldID, n.ID)
	if err != nil {
		return err
	}

	_, err = tx.ExecContext(ctx, `
		UPDATE negotiation_events
		SET status = ?
		WHERE world_id = ? AND id = ?
	`, processedStatus, worldID, job.EventID)
	if err != nil {
		return err
	}

	// 回应关联原邀请。这里只保存面向玩家的回应记录，
	// 不代表另一位 NPC 自动获知了这段话。
	_, err = tx.ExecContext(ctx, `
		INSERT INTO negotiation_events (
			world_id,
			id,
			negotiation_id,
			actor_kind,
			actor_id,
			type,
			source_event_id,
			negotiation_revision,
			text,
			status
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 'done')
	`,
		worldID,
		job.EventID+":response",
		n.ID,
		actorKind,
		actorID,
		eventType,
		job.EventID,
		n.Revision+1,
		decision.Speech,
	)
	if err != nil {
		return err
	}

	// 一人拒绝或处理失败，本次会面停止。
	// 取消未处理的邀请，不继续消耗模型调用。
	if nextStatus != NegotiationStatusInviting {
		_, err = tx.ExecContext(ctx, `
			UPDATE negotiation_events
			SET status = 'cancelled'
			WHERE world_id = ?
				AND negotiation_id = ?
				AND status = 'pending'
		`, worldID, n.ID)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

func RecoverNegotiationInvitations(
	ctx context.Context,
	db *sql.DB,
) error {
	rows, err := db.QueryContext(ctx, `
		SELECT world_id, negotiation_id, id, recipient_id
		FROM negotiation_events
		WHERE type = 'invitation'
			AND status = 'processing'
		ORDER BY seq
	`)
	if err != nil {
		return err
	}

	var jobs []NegotiationInvitation

	for rows.Next() {
		var job NegotiationInvitation

		if err := rows.Scan(
			&job.Negotiation.WorldID,
			&job.Negotiation.ID,
			&job.EventID,
			&job.NPCID,
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

	// 必须先关闭 rows，再启动写入事务。
	// 当前数据库只有一个连接。
	for _, job := range jobs {
		err := FinishNegotiationInvitation(
			ctx,
			db,
			job,
			InvitationDecision{
				Action: "failed",
				Speech: "服务重启中断了邀请处理，本次会面未继续。",
			},
		)
		if err != nil {
			return err
		}
	}

	return nil
}
