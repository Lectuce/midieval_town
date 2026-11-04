package negotiation

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"time"

	"cybertown/internal/game/actor"
	constError "cybertown/internal/game/const_error"
	gamenegotiation "cybertown/internal/game/negotiation"
	"cybertown/internal/game/repair"
	"cybertown/internal/npc"
)

type Service struct {
	DB           *sql.DB
	Agent        *npc.Agent
	MeetingRoute gamenegotiation.NegotiationMeetingRoute
}

func (s *Service) Run(ctx context.Context) {
	// 启动时也检查，处理上次已经接受但尚未出发的会话。
	if err := s.startReadyMeetings(ctx); err != nil {
		log.Printf("meeting recovery stopped: %v", err)
		return
	}

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return

		case <-ticker.C:
			err := s.processOne(ctx)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				if ctx.Err() == nil {
					log.Printf("negotiation worker stopped: %v", err)
				}
				return
			}

			// 即使没有新邀请，也检查是否可以出发。
			if err := s.startReadyMeetings(ctx); err != nil {
				if ctx.Err() == nil {
					log.Printf("meeting worker stopped: %v", err)
				}
				return
			}
		}
	}
}

func (s *Service) processOne(ctx context.Context) error {
	job, err := gamenegotiation.ClaimNegotiationInvitation(ctx, s.DB)
	if err != nil {
		return err
	}

	callCtx, cancel := context.WithTimeout(ctx, 30*time.Second)

	character, workErr := actor.GetNPC(
		callCtx,
		s.DB,
		job.Negotiation.WorldID,
		job.NPCID,
	)

	var decision gamenegotiation.InvitationDecision

	if workErr == nil {
		job.NPCRevision = character.Revision

		// 调用前先排除已经变化的报价，避免无效模型调用。
		proposal, err := repair.GetRepairProposal(
			callCtx,
			s.DB,
			job.Negotiation.WorldID,
			job.Negotiation.CurrentProposalID,
		)
		workErr = err

		if workErr == nil &&
			(proposal.Revision != job.Negotiation.CurrentProposalRevision ||
				proposal.Status != repair.RepairProposalStatusProposed ||
				proposal.PayerConfirmed ||
				!proposal.WorkerConfirmed) {
			workErr = constError.ErrNegotiationUnavailable
		}
	}

	if workErr == nil {
		decision, workErr = s.Agent.RespondToNegotiation(
			callCtx,
			character,
			job,
		)
	}

	cancel()

	if workErr != nil {
		log.Printf(
			"negotiation invitation failed session=%s event=%s npc=%s error=%v",
			job.Negotiation.ID,
			job.EventID,
			job.NPCID,
			workErr,
		)

		decision = gamenegotiation.InvitationDecision{
			Action: "failed",
			Speech: "本次邀请处理失败，尚未取得角色决定。",
		}
	}

	// 模型超时或关闭服务时，仍留出短时间保存处理结果。
	saveCtx, cancelSave := context.WithTimeout(
		context.WithoutCancel(ctx),
		5*time.Second,
	)
	defer cancelSave()

	err = gamenegotiation.FinishNegotiationInvitation(
		saveCtx,
		s.DB,
		job,
		decision,
	)
	if err != nil {
		return err
	}

	// 不把模型请求的 action 当成最终落地状态打印。
	// 因为保存时可能检测到版本变化而转为 failed。
	log.Printf(
		"negotiation invitation processed session=%s event=%s npc=%s",
		job.Negotiation.ID,
		job.EventID,
		job.NPCID,
	)

	return nil
}

func (s *Service) startReadyMeetings(ctx context.Context) error {
	meetings, err := gamenegotiation.ListReadyMeetings(ctx, s.DB)
	if err != nil {
		return err
	}

	for _, meeting := range meetings {
		err := gamenegotiation.StartNegotiationMeeting(
			ctx,
			s.DB,
			meeting.WorldID,
			meeting.ID,
			s.MeetingRoute,
		)
		if err == nil {
			log.Printf(
				"meeting started negotiation=%s",
				meeting.ID,
			)
			continue
		}

		if ctx.Err() != nil {
			return ctx.Err()
		}

		// 具体原因保留在服务端日志中。
		log.Printf(
			"meeting start failed negotiation=%s error=%v",
			meeting.ID,
			err,
		)

		if err := gamenegotiation.BlockUnstartedMeeting(
			ctx,
			s.DB,
			meeting,
		); err != nil {
			return err
		}
	}

	return nil
}
