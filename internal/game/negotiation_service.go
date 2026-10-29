package game

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
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
			next_actor_id,
			round_count,
			director_calls,
			revision,
			end_reason,
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
		&n.EndReason,
		&n.CreatedAt,
	)

	return n, err
}
