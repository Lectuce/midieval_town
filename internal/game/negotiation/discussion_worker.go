package negotiation

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"cybertown/internal/game/repair"
)

func ClaimDiscussion(
	ctx context.Context,
	db *sql.DB,
) (DiscussionWork, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return DiscussionWork{}, err
	}
	defer tx.Rollback()

	var w DiscussionWork
	var worldID, negotiationID string

	err = tx.QueryRowContext(ctx, `
		SELECT
			e.world_id, e.negotiation_id, e.id,
			COALESCE(e.source_event_id, ''),
			COALESCE(e.recipient_id, ''), d.kind, d.topic
		FROM negotiation_events e
		JOIN negotiation_discussion_data d
			ON d.world_id = e.world_id AND d.event_id = e.id
		JOIN negotiations n
			ON n.world_id = e.world_id AND n.id = e.negotiation_id
		WHERE e.status = 'pending'
			AND n.status = 'discussing'
			AND d.kind IN ('review', 'clarify', 'counter', 'director')
			AND NOT EXISTS (
				SELECT 1 FROM negotiation_events running
				WHERE running.world_id = e.world_id
					AND running.negotiation_id = e.negotiation_id
					AND running.status = 'processing'
			)
		ORDER BY e.seq
		LIMIT 1
	`).Scan(
		&worldID, &negotiationID, &w.EventID, &w.SourceID,
		&w.Input.ActorID, &w.Input.Kind, &w.Input.Topic,
	)
	if err != nil {
		return w, err
	}

	n, err := getNegotiationTx(ctx, tx, worldID, negotiationID)
	if err != nil {
		return w, err
	}
	p, err := discussionReadyTx(ctx, tx, n)
	if errors.Is(err, ErrDiscussionState) {
		if err := endDiscussionTx(
			ctx, tx, n, "blocked", "角色、报价或货车状态发生变化，本次商量停止。",
		); err != nil {
			return w, err
		}
		if err := tx.Commit(); err != nil {
			return w, err
		}
		return w, sql.ErrNoRows
	}
	if err != nil {
		return w, err
	}

	var calls int
	err = tx.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(e.attempts), 0)
		FROM negotiation_events e
		JOIN negotiation_discussion_data d
			ON d.world_id = e.world_id AND d.event_id = e.id
		WHERE e.world_id = ? AND e.negotiation_id = ?
			AND d.kind IN ('review', 'clarify', 'counter', 'director')
	`, worldID, n.ID).Scan(&calls)
	if err != nil {
		return w, err
	}

	exhausted := calls >= MaxDiscussionCalls ||
		(w.Input.Kind == DiscussionReview &&
			n.RoundCount >= MaxDiscussionReviews) ||
		(w.Input.Kind == DiscussionDirector &&
			n.DirectorCalls >= MaxDirectorCalls)

	if exhausted {
		if err := endDiscussionTx(
			ctx, tx, n, "blocked", "这次商量暂时没有达成一致。",
		); err != nil {
			return w, err
		}
		if err := tx.Commit(); err != nil {
			return w, err
		}
		return w, sql.ErrNoRows
	}

	// 插话只在新发言开始前送达，不修改已经在生成中的发言。
	_, err = tx.ExecContext(ctx, `
		UPDATE negotiation_events
		SET status = 'done'
		WHERE world_id = ? AND negotiation_id = ?
			AND type = 'player_message' AND status = 'pending'
	`, worldID, n.ID)
	if err != nil {
		return w, err
	}
	_, err = tx.ExecContext(ctx, `
		UPDATE negotiation_discussion_data
		SET public = 1
		WHERE world_id = ? AND kind = 'player'
			AND event_id IN (
				SELECT id FROM negotiation_events
				WHERE world_id = ? AND negotiation_id = ?
					AND type = 'player_message' AND status = 'done'
			)
	`, worldID, worldID, n.ID)
	if err != nil {
		return w, err
	}

	w.Input.Proposal = p
	w.Input.History, err = discussionHistoryTx(ctx, tx, worldID, n.ID)
	if err != nil {
		return w, err
	}

	if w.Input.Kind == DiscussionDirector {
		w.Input.ActorID = "town-director"
	} else {
		self, err := discussionSelfTx(ctx, tx, worldID, w.Input.ActorID)
		if err != nil {
			return w, err
		}
		w.Input.Self = &self
	}

	w.Input.AllowedActions = AllowedDiscussionActions(
		w.Input.Kind, w.Input.ActorID,
	)
	if len(w.Input.AllowedActions) == 0 {
		return w, ErrDiscussionInput
	}

	result, err := tx.ExecContext(ctx, `
		UPDATE negotiation_events
		SET status = 'processing', attempts = attempts + 1
		WHERE world_id = ? AND id = ? AND status = 'pending'
	`, worldID, w.EventID)
	if err != nil {
		return w, err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return w, err
	}
	if count != 1 {
		return w, ErrDiscussionState
	}

	reviewIncrement, directorIncrement := 0, 0
	if w.Input.Kind == DiscussionReview {
		reviewIncrement = 1
	}
	if w.Input.Kind == DiscussionDirector {
		directorIncrement = 1
	}

	_, err = tx.ExecContext(ctx, `
		UPDATE negotiations
		SET revision = revision + 1,
			round_count = round_count + ?,
			director_calls = director_calls + ?
		WHERE world_id = ? AND id = ?
	`, reviewIncrement, directorIncrement, worldID, n.ID)
	if err != nil {
		return w, err
	}

	n.Revision++
	n.RoundCount += reviewIncrement
	n.DirectorCalls += directorIncrement
	w.Negotiation = n
	w.Input.RemainingReviews = MaxDiscussionReviews - n.RoundCount
	w.Input.RemainingCalls = MaxDiscussionCalls - calls - 1

	if err := tx.Commit(); err != nil {
		return w, err
	}
	return w, nil
}

func FinishDiscussion(
	ctx context.Context,
	db *sql.DB,
	w DiscussionWork,
	d DiscussionDecision,
) error {
	if err := ValidateDiscussionDecision(w.Input, d); err != nil {
		return err
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	n, err := getNegotiationTx(
		ctx, tx, w.Negotiation.WorldID, w.Negotiation.ID,
	)
	if err != nil {
		return err
	}

	var eventStatus string
	err = tx.QueryRowContext(ctx, `
		SELECT status FROM negotiation_events
		WHERE world_id = ? AND id = ?
	`, n.WorldID, w.EventID).Scan(&eventStatus)
	if err != nil {
		return err
	}

	// 相同结果保存重试，不重复确认、不重复创建报价。
	if eventStatus == "done" {
		return nil
	}
	if eventStatus != "processing" ||
		n.Status != "discussing" ||
		n.Revision != w.Negotiation.Revision {
		return ErrDiscussionState
	}

	p, err := discussionReadyTx(ctx, tx, n)
	if err != nil {
		return err
	}
	if p.ID != w.Input.Proposal.ID ||
		p.Revision != w.Input.Proposal.Revision {
		return ErrDiscussionState
	}

	if w.Input.Self != nil {
		current, err := discussionSelfTx(ctx, tx, n.WorldID, w.Input.ActorID)
		if err != nil {
			return err
		}
		if current != *w.Input.Self {
			return ErrDiscussionState
		}
	}

	_, err = tx.ExecContext(ctx, `
		UPDATE negotiation_events SET status = 'done'
		WHERE world_id = ? AND id = ?
	`, n.WorldID, w.EventID)
	if err != nil {
		return err
	}

	responseID := discussionID(w.EventID, "response")

	if w.Input.Kind == DiscussionDirector {
		if err := applyDiscussionPlanTx(
			ctx, tx, n, w, d, responseID,
		); err != nil {
			return err
		}
	} else {
		if err := applyNPCDecisionTx(
			ctx, tx, n, w, d, responseID,
		); err != nil {
			return err
		}
	}

	_, err = tx.ExecContext(ctx, `
		UPDATE negotiations SET revision = revision + 1
		WHERE world_id = ? AND id = ? AND status = 'discussing'
	`, n.WorldID, n.ID)
	if err != nil {
		return err
	}

	return tx.Commit()
}

func applyNPCDecisionTx(
	ctx context.Context,
	tx *sql.Tx,
	n Negotiation,
	w DiscussionWork,
	d DiscussionDecision,
	responseID string,
) error {
	p := w.Input.Proposal

	if d.Action == "counteroffer" {
		t := d.Terms
		if t.PriceCoins == p.PriceCoins &&
			t.MaterialCount == p.MaterialCount &&
			t.DurationMinutes == p.DurationMinutes {
			return ErrDiscussionInput
		}
		if w.Input.Self == nil || w.Input.Self.Materials < t.MaterialCount {
			return ErrDiscussionState
		}

		newProposal, err := repair.CreateRepairProposalTx(
			ctx, tx, n.WorldID,
			repair.CreateRepairProposalRequest{
				ID:              discussionID(w.EventID, "proposal"),
				PriceCoins:      t.PriceCoins,
				MaterialCount:   t.MaterialCount,
				DurationMinutes: t.DurationMinutes,
			},
			n.ID,
		)
		if err != nil {
			return err
		}

		newProposal, err = repair.ConfirmRepairProposalTx(
			ctx, tx, n.WorldID, newProposal.ID,
			"blacksmith", newProposal.Revision, n.ID,
		)
		if err != nil {
			return err
		}

		result, err := tx.ExecContext(ctx, `
			UPDATE repair_proposals
			SET status = 'cancelled', revision = revision + 1
			WHERE world_id = ? AND id = ?
				AND revision = ? AND status = 'proposed'
		`, n.WorldID, p.ID, p.Revision)
		if err != nil {
			return err
		}
		count, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if count != 1 {
			return ErrDiscussionState
		}

		n.CurrentProposalID = newProposal.ID
		n.CurrentProposalRevision = newProposal.Revision

		_, err = tx.ExecContext(ctx, `
			UPDATE negotiations
			SET current_proposal_id = ?, current_proposal_revision = ?
			WHERE world_id = ? AND id = ?
		`, newProposal.ID, newProposal.Revision, n.WorldID, n.ID)
		if err != nil {
			return err
		}

		if err := deliverDiscussionProposalTx(
			ctx, tx, n, responseID,
		); err != nil {
			return err
		}
	}

	if err := insertDiscussionEventTx(
		ctx, tx, n, responseID, w.Input.ActorID, "",
		"proposal_response", w.EventID,
		d.Speech, "done", "speech", d.Topic, true,
		discussionPayload{Decision: &d},
	); err != nil {
		return err
	}

	switch d.Action {
	case "accept_proposal":
		// 权威层检查双方资源，不能让模型用台词绕过。
		payer, err := discussionSelfTx(ctx, tx, n.WorldID, "merchant")
		if err != nil {
			return err
		}
		worker, err := discussionSelfTx(ctx, tx, n.WorldID, "blacksmith")
		if err != nil {
			return err
		}
		if payer.Coins < p.PriceCoins || worker.Materials < p.MaterialCount {
			return endDiscussionTx(
				ctx, tx, n, "blocked",
				"当前资源不足以支持这份报价，尚未确认协议。",
			)
		}

		confirmed, err := repair.ConfirmRepairProposalTx(
			ctx, tx, n.WorldID, p.ID, "merchant", p.Revision, n.ID,
		)
		if err != nil {
			return err
		}

		_, err = tx.ExecContext(ctx, `
			UPDATE negotiations
			SET current_proposal_revision = ?
			WHERE world_id = ? AND id = ?
		`, confirmed.Revision, n.WorldID, n.ID)
		if err != nil {
			return err
		}
		n.CurrentProposalRevision = confirmed.Revision

		return endDiscussionTx(
			ctx, tx, n, "agreed",
			"双方已同意这份维修报价。尚未扣款，也尚未开工。",
		)

	case "withdraw":
		return endDiscussionTx(
			ctx, tx, n, "blocked", "一方明确退出，本次商量结束。",
		)

	case "request_change":
		// 同一报价下重复相同分歧，不再反复调用导演。
		var repeated bool
		err := tx.QueryRowContext(ctx, `
			SELECT EXISTS (
				SELECT 1
				FROM negotiation_events e
				JOIN negotiation_discussion_data m
					ON m.world_id = e.world_id AND m.event_id = e.id
				WHERE e.world_id = ? AND e.negotiation_id = ?
					AND e.id != ? AND e.proposal_id = ?
					AND m.kind = 'speech'
					AND json_extract(m.payload_json, '$.decision.action') = 'request_change'
					AND json_extract(m.payload_json, '$.decision.topic') = ?
					AND json_extract(m.payload_json, '$.decision.reason') = ?
			)
		`, n.WorldID, n.ID, responseID, p.ID, d.Topic, d.Reason).Scan(&repeated)
		if err != nil {
			return err
		}
		if repeated {
			return endDiscussionTx(
				ctx, tx, n, "blocked", "同一分歧没有变化，本次商量暂时停止。",
			)
		}
		return queueDiscussionTx(
			ctx, tx, n, responseID, "", DiscussionDirector, d.Topic,
		)

	case "counteroffer":
		return queueDiscussionTx(
			ctx, tx, n, responseID, "merchant", DiscussionReview, "",
		)

	case "clarify":
		if w.Input.ActorID == "blacksmith" {
			return queueDiscussionTx(
				ctx, tx, n, responseID, "merchant", DiscussionReview, d.Topic,
			)
		}
		return queueDiscussionTx(
			ctx, tx, n, responseID, "", DiscussionDirector, d.Topic,
		)
	}

	return ErrDiscussionInput
}

func applyDiscussionPlanTx(
	ctx context.Context,
	tx *sql.Tx,
	n Negotiation,
	w DiscussionWork,
	d DiscussionDecision,
	responseID string,
) error {
	if err := insertDiscussionEventTx(
		ctx, tx, n, responseID, "town-director", "",
		"director_plan", w.EventID, "",
		"done", "plan", d.Topic, false,
		discussionPayload{Decision: &d},
	); err != nil {
		return err
	}

	if d.Action == "end" {
		return endDiscussionTx(
			ctx, tx, n, "blocked", "目前没有找到双方愿意接受的条件，本次商量结束。",
		)
	}

	var repeated bool
	err := tx.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM negotiation_events e
			JOIN negotiation_discussion_data m
				ON m.world_id = e.world_id AND m.event_id = e.id
			WHERE e.world_id = ? AND e.negotiation_id = ?
				AND e.proposal_id = ? AND e.id != ?
				AND m.kind = 'plan'
				AND json_extract(m.payload_json, '$.decision.action') = ?
				AND json_extract(m.payload_json, '$.decision.target') = ?
				AND json_extract(m.payload_json, '$.decision.topic') = ?
		)
	`, n.WorldID, n.ID, n.CurrentProposalID, responseID,
		d.Action, d.Target, d.Topic,
	).Scan(&repeated)
	if err != nil {
		return err
	}
	if repeated {
		return endDiscussionTx(
			ctx, tx, n, "blocked", "同样的安排已经尝试过，本次商量暂时停止。",
		)
	}

	name := "马丁"
	if d.Target == "blacksmith" {
		name = "罗兰"
	}
	topics := map[string]string{
		"price":     "工钱",
		"materials": "材料",
		"duration":  "工时",
	}

	kind := DiscussionClarify
	text := fmt.Sprintf("接下来请%s说明%s方面的顾虑或余地。", name, topics[d.Topic])
	if d.Action == "request_counteroffer" {
		kind = DiscussionCounter
		text = fmt.Sprintf("接下来请罗兰考虑%s方面是否能提出另一份报价。", topics[d.Topic])
	}

	arrangementID := discussionID(responseID, "arrangement")
	if err := insertDiscussionEventTx(
		ctx, tx, n, arrangementID, "game_service", "",
		"proposal_response", responseID, text,
		"done", "arrangement", d.Topic, true, discussionPayload{},
	); err != nil {
		return err
	}

	return queueDiscussionTx(
		ctx, tx, n, arrangementID, d.Target, kind, d.Topic,
	)
}

func FailDiscussion(
	ctx context.Context,
	db *sql.DB,
	worldID, negotiationID, eventID string,
	reason string,
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

	var status string
	err = tx.QueryRowContext(ctx, `
		SELECT status FROM negotiation_events
		WHERE world_id = ? AND id = ? AND negotiation_id = ?
	`, worldID, eventID, negotiationID).Scan(&status)
	if err != nil {
		return err
	}
	if status != "processing" || n.Status != "discussing" {
		return nil
	}

	_, err = tx.ExecContext(ctx, `
		UPDATE negotiation_events SET status = 'failed'
		WHERE world_id = ? AND id = ?
	`, worldID, eventID)
	if err != nil {
		return err
	}
	if err := endDiscussionTx(ctx, tx, n, "failed", reason); err != nil {
		return err
	}
	return tx.Commit()
}

func RecoverDiscussions(ctx context.Context, db *sql.DB) error {
	rows, err := db.QueryContext(ctx, `
		SELECT e.world_id, e.negotiation_id, e.id
		FROM negotiation_events e
		JOIN negotiation_discussion_data d
			ON d.world_id = e.world_id AND d.event_id = e.id
		WHERE e.status = 'processing'
			AND d.kind IN ('review', 'clarify', 'counter', 'director')
	`)
	if err != nil {
		return err
	}

	var jobs [][3]string
	for rows.Next() {
		var job [3]string
		if err := rows.Scan(&job[0], &job[1], &job[2]); err != nil {
			rows.Close()
			return err
		}
		jobs = append(jobs, job)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}

	for _, job := range jobs {
		if err := FailDiscussion(
			ctx, db, job[0], job[1], job[2],
			"服务重启中断了这次发言，协商没有自动重试。",
		); err != nil {
			return err
		}
	}
	return nil
}
