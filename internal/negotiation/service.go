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
	DB    *sql.DB
	Agent *npc.Agent
}

func (s *Service) Run(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return

		case <-ticker.C:
			if err := s.processOne(ctx); err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					continue
				}
				if ctx.Err() != nil {
					return
				}

				// 数据库领取/落地失败时停止此 worker。
				// 避免留下 processing 后仍不断处理其他事件。
				log.Printf("negotiation worker stopped: %v", err)
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
