package npc

import (
	"bytes"
	"context"
	"cybertown/internal/agent/engine"
	"cybertown/internal/game"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
)

func getReceivedRepairProposals(
	ctx context.Context,
	db *sql.DB,
	scope engine.Scope,
	input map[string]any,
) (string, error) {
	if scope.AgentKind != "npc" || scope.AgentID != "merchant" {
		return "", fmt.Errorf("only the merchant can use this tool")
	}

	proposals, err := game.GetReceivedRepairProposals(
		ctx, db, scope.WorldID, scope.AgentID,
	)
	if err != nil {
		return "", err
	}

	items, err := game.GetNPCInventory(
		ctx, db, scope.WorldID, scope.AgentID,
	)
	if err != nil {
		return "", err
	}

	var coins int64
	for _, item := range items {
		if item.ItemID == "coin" {
			coins = item.Quantity
			break
		}
	}

	// 同一次查询返回本人资金，减少额外模型轮次。
	// 不提供铁匠或玩家的库存。
	raw, err := json.Marshal(struct {
		Source    string                `json:"source"`
		OwnCoins  int64                 `json:"own_coins"`
		Proposals []game.RepairProposal `json:"proposals"`
	}{
		Source:    "玩家明确转交的报价",
		OwnCoins:  coins,
		Proposals: proposals,
	})
	if err != nil {
		return "", err
	}

	return string(raw), nil
}

func acceptRepairProposal(
	ctx context.Context,
	db *sql.DB,
	scope engine.Scope,
	input map[string]any,
) (string, error) {
	if scope.AgentKind != "npc" || scope.AgentID != "merchant" {
		return "", fmt.Errorf("only the merchant can accept payment terms")
	}

	var args struct {
		ProposalID       string `json:"proposal_id"`
		ExpectedRevision int64  `json:"expected_revision"`
	}

	raw, err := json.Marshal(input)
	if err != nil {
		return "", err
	}

	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(&args); err != nil {
		return "", fmt.Errorf("invalid repair acceptance input: %w", err)
	}
	if args.ProposalID == "" || args.ExpectedRevision < 1 {
		return "", fmt.Errorf("proposal id and revision are required")
	}

	proposal, err := game.ConfirmRepairProposal(
		ctx,
		db,
		scope.WorldID,
		args.ProposalID,
		scope.AgentID, // 身份来自服务端，不来自模型参数。
		args.ExpectedRevision,
	)
	if err != nil {
		return "", err
	}

	log.Printf(
		"NPC repair acceptance turn=%s npc=%s proposal=%s "+
			"payer_confirmed=%t worker_confirmed=%t status=%s",
		scope.TurnID,
		scope.AgentID,
		proposal.ID,
		proposal.PayerConfirmed,
		proposal.WorkerConfirmed,
		proposal.Status,
	)

	raw, err = json.Marshal(proposal)
	if err != nil {
		return "", err
	}

	return string(raw), nil
}

func respondToRepairProposal(
	ctx context.Context,
	db *sql.DB,
	scope engine.Scope,
	input map[string]any,
) (string, error) {
	if scope.AgentKind != "npc" || scope.AgentID != "merchant" {
		return "", fmt.Errorf("only the merchant can use this tool")
	}

	var args struct {
		ProposalID       string `json:"proposal_id"`
		ExpectedRevision int64  `json:"expected_revision"`
		Decision         string `json:"decision"`
		Reason           string `json:"reason"`
	}

	raw, err := json.Marshal(input)
	if err != nil {
		return "", err
	}

	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(&args); err != nil {
		return "", err
	}

	event, err := game.RecordRepairResponse(
		ctx,
		db,
		scope.WorldID,
		scope.PlayerID,
		scope.AgentID,
		scope.TurnID,
		args.ProposalID,
		args.ExpectedRevision,
		args.Decision,
		args.Reason,
	)
	if err != nil {
		return "", err
	}

	log.Printf(
		"NPC repair response turn=%s event=%s decision=%s status=%s",
		scope.TurnID,
		event.ID,
		event.Decision,
		event.Status,
	)

	raw, err = json.Marshal(event)
	if err != nil {
		return "", err
	}

	return string(raw), nil
}
