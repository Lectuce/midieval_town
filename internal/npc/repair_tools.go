package npc

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"

	"cybertown/internal/agent/engine"
	"cybertown/internal/game"
)

func getMyRepairResources(
	ctx context.Context,
	db *sql.DB,
	scope engine.Scope,
	input map[string]any,
) (string, error) {
	if scope.AgentKind != "npc" {
		return "", fmt.Errorf("this tool requires an NPC scope")
	}

	if scope.AgentID != "blacksmith" &&
		scope.AgentID != "merchant" {
		return "", fmt.Errorf(
			"this NPC cannot use repair resource tools",
		)
	}

	items, err := game.GetNPCInventory(
		ctx,
		db,
		scope.WorldID,
		scope.AgentID,
	)
	if err != nil {
		return "", err
	}

	raw, err := json.Marshal(items)
	if err != nil {
		return "", fmt.Errorf(
			"encode repair resources: %w", err,
		)
	}

	return string(raw), nil
}

func proposeCartRepair(
	ctx context.Context,
	db *sql.DB,
	scope engine.Scope,
	input map[string]any,
) (string, error) {
	if scope.AgentKind != "npc" ||
		scope.AgentID != "blacksmith" {
		return "", fmt.Errorf(
			"only the blacksmith can propose this repair",
		)
	}

	if scope.WorldID == "" ||
		scope.PlayerID == "" ||
		scope.TurnID == "" {
		return "", fmt.Errorf(
			"world, player and turn context are required",
		)
	}

	// 使用指针区分“没有传参数”和“费用为零”。
	var args struct {
		PriceCoins      *int64 `json:"price_coins"`
		MaterialCount   *int64 `json:"material_count"`
		DurationMinutes *int64 `json:"duration_minutes"`
	}

	rawInput, err := json.Marshal(input)
	if err != nil {
		return "", fmt.Errorf(
			"encode repair tool input: %w", err,
		)
	}

	decoder := json.NewDecoder(bytes.NewReader(rawInput))
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(&args); err != nil {
		return "", fmt.Errorf(
			"invalid repair tool input: %w", err,
		)
	}

	if args.PriceCoins == nil ||
		args.MaterialCount == nil ||
		args.DurationMinutes == nil {
		return "", fmt.Errorf(
			"price_coins, material_count and duration_minutes are required",
		)
	}

	if *args.PriceCoins < 0 ||
		*args.MaterialCount < 1 ||
		*args.DurationMinutes < 1 ||
		*args.DurationMinutes > 1440 {
		return "", fmt.Errorf(
			"invalid repair proposal terms",
		)
	}

	// 同一角色、同一玩家、同一轮使用同一个提议 ID。
	// 网络重试不会每次创建一个新报价。
	key := fmt.Sprintf(
		"%q/%q/%q/%q",
		scope.WorldID,
		scope.PlayerID,
		scope.AgentID,
		scope.TurnID,
	)
	sum := sha256.Sum256([]byte(key))
	proposalID := fmt.Sprintf("repair-offer-%x", sum[:16])

	proposal, err := game.GetRepairProposal(
		ctx,
		db,
		scope.WorldID,
		proposalID,
	)

	switch {
	case err == nil:
		// 重试只能复用相同条款，不能覆盖已经保存的报价。
		if proposal.PriceCoins != *args.PriceCoins ||
			proposal.MaterialCount != *args.MaterialCount ||
			proposal.DurationMinutes != *args.DurationMinutes {
			return "", fmt.Errorf(
				"this turn already created a different repair proposal",
			)
		}

	case errors.Is(err, sql.ErrNoRows):
		proposal, err = game.CreateRepairProposal(
			ctx,
			db,
			scope.WorldID,
			game.CreateRepairProposalRequest{
				ID:              proposalID,
				PriceCoins:      *args.PriceCoins,
				MaterialCount:   *args.MaterialCount,
				DurationMinutes: *args.DurationMinutes,
			},
		)
		if err != nil {
			return "", err
		}

	default:
		return "", err
	}

	if proposal.WorkerNPCID != scope.AgentID {
		return "", fmt.Errorf(
			"proposal does not belong to this repair worker",
		)
	}

	// 本工具的含义包含“罗兰愿意按这份条件维修”。
	// 因此只确认当前角色自己，不确认付款方。
	if !proposal.WorkerConfirmed {
		// 确认前检查本人当前是否有足够材料。
		// 这不是预留材料，开工时仍需重新检查。
		items, err := game.GetNPCInventory(
			ctx,
			db,
			scope.WorldID,
			scope.AgentID,
		)
		if err != nil {
			return "", err
		}

		var availableMaterials int64
		for _, item := range items {
			if item.ItemID == "repair_material" {
				availableMaterials = item.Quantity
				break
			}
		}

		if availableMaterials < proposal.MaterialCount {
			return "", fmt.Errorf(
				"you have %d repair materials, but this proposal requires %d",
				availableMaterials,
				proposal.MaterialCount,
			)
		}

		proposal, err = game.ConfirmRepairProposal(
			ctx,
			db,
			scope.WorldID,
			proposal.ID,
			scope.AgentID,
			proposal.Revision,
		)
		if err != nil {
			return "", err
		}
	}

	_, err = db.ExecContext(ctx, `
		INSERT INTO repair_proposal_deliveries (
			world_id,
			proposal_id,
			player_id,
			recipient_npc_id,
			source_turn_id
		)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (
			world_id, proposal_id, player_id, recipient_npc_id
		) DO NOTHING
	`,
		proposal.WorldID,
		proposal.ID,
		scope.PlayerID,
		proposal.PayerNPCID,
		scope.TurnID,
	)
	if err != nil {
		return "", fmt.Errorf(
			"record repair proposal delivery source: %w",
			err,
		)
	}

	log.Printf(
		"NPC repair offer turn=%s npc=%s proposal=%s "+
			"price=%d materials=%d minutes=%d "+
			"worker_confirmed=%t payer_confirmed=%t status=%s",
		scope.TurnID,
		scope.AgentID,
		proposal.ID,
		proposal.PriceCoins,
		proposal.MaterialCount,
		proposal.DurationMinutes,
		proposal.WorkerConfirmed,
		proposal.PayerConfirmed,
		proposal.Status,
	)

	raw, err := json.Marshal(proposal)
	if err != nil {
		return "", fmt.Errorf(
			"encode repair proposal: %w", err,
		)
	}

	return string(raw), nil
}
