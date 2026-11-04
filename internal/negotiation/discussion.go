package negotiation

import (
	"context"
	"errors"
	"log"
	"time"

	gamenegotiation "cybertown/internal/game/negotiation"
)

func (s *Service) processDiscussion(ctx context.Context) error {
	if err := gamenegotiation.EnsureAllDiscussions(ctx, s.DB); err != nil {
		return err
	}

	work, err := gamenegotiation.ClaimDiscussion(ctx, s.DB)
	if err != nil {
		return err
	}

	callCtx, cancel := context.WithTimeout(ctx, 30*time.Second)

	var decision gamenegotiation.DiscussionDecision
	if work.Input.Kind == gamenegotiation.DiscussionDirector {
		decision, err = s.Director.PlanDiscussion(callCtx, work)
	} else {
		decision, err = s.Agent.RespondToDiscussion(callCtx, work)
	}
	cancel()

	saveCtx, saveCancel := context.WithTimeout(
		context.WithoutCancel(ctx), 5*time.Second,
	)
	defer saveCancel()

	if err == nil {
		err = gamenegotiation.FinishDiscussion(
			saveCtx, s.DB, work, decision,
		)
	}

	if err != nil {
		log.Printf(
			"discussion failed session=%s event=%s actor=%s error=%v",
			work.Negotiation.ID, work.EventID, work.Input.ActorID, err,
		)

		reason := "本次发言处理失败，尚未取得有效决定。"
		if errors.Is(err, gamenegotiation.ErrDiscussionState) {
			reason = "处理期间会面或报价状态已经变化，本次决定没有执行。"
		}

		return gamenegotiation.FailDiscussion(
			saveCtx, s.DB,
			work.Negotiation.WorldID,
			work.Negotiation.ID,
			work.EventID,
			reason,
		)
	}

	log.Printf(
		"discussion saved session=%s event=%s actor=%s action=%s topic=%s",
		work.Negotiation.ID, work.EventID,
		work.Input.ActorID, decision.Action, decision.Topic,
	)
	return nil
}
