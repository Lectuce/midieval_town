package game

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
)

type ItemClaimResult struct {
	SourceID     string `json:"source_id"`
	ItemID       string `json:"item_id"`
	NewlyClaimed bool   `json:"newly_claimed"`
	Quantity     int64  `json:"quantity"`
	Message      string `json:"message"`
}

var ErrEvidenceRequired = errors.New("required evidence not found")

func ClaimShipmentReceipt(
	ctx context.Context,
	db *sql.DB,
	worldID string,
	playerID string,
) (ItemClaimResult, error) {
	if worldID == "" || playerID == "" {
		return ItemClaimResult{}, fmt.Errorf(
			"world id and player id are required",
		)
	}

	const sourceID = "abandoned_cart"
	const itemID = "shipment_receipt"

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return ItemClaimResult{}, fmt.Errorf("begin item claim: %w", err)
	}
	defer tx.Rollback()

	// 确认玩家存在，读取当前状态和位置。
	var player Player

	err = tx.QueryRowContext(ctx, `
		SELECT scene_id, position_x, position_y, floor, status
		FROM players
		WHERE world_id = ? AND id = ?
	`, worldID, playerID).Scan(
		&player.SceneID,
		&player.Position.X,
		&player.Position.Y,
		&player.Floor,
		&player.Status,
	)
	if err != nil {
		return ItemClaimResult{}, fmt.Errorf("load claim player: %w", err)
	}

	// 检查是否已经从这个来源领取过。
	var claimed bool

	err = tx.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM player_item_claims
			WHERE world_id = ?
				AND player_id = ?
				AND source_id = ?
				AND item_id = ?
		)
	`, worldID, playerID, sourceID, itemID).Scan(&claimed)
	if err != nil {
		return ItemClaimResult{}, fmt.Errorf("load item claim: %w", err)
	}

	newlyClaimed := false

	// 已领取的请求只返回当前结果，不再授予物品。
	if !claimed {
		// 首次领取必须满足玩家、任务、证据和位置条件。
		if player.Status != "active" {
			return ItemClaimResult{}, ErrPlayerInactive
		}

		var questStatus string

		err = tx.QueryRowContext(ctx, `
			SELECT status
			FROM player_quests
			WHERE world_id = ?
				AND player_id = ?
				AND quest_id = ?
		`, worldID, playerID, "missing_iron_cart").Scan(&questStatus)

		if errors.Is(err, sql.ErrNoRows) {
			return ItemClaimResult{}, ErrQuestNotActive
		}
		if err != nil {
			return ItemClaimResult{}, fmt.Errorf("load claim quest: %w", err)
		}
		if questStatus != QuestStatusActive {
			return ItemClaimResult{}, ErrQuestNotActive
		}

		var hasEvidence bool

		err = tx.QueryRowContext(ctx, `
			SELECT EXISTS (
				SELECT 1
				FROM player_evidence
				WHERE world_id = ?
					AND player_id = ?
					AND evidence_id = ?
			)
		`, worldID, playerID, "cart_found").Scan(&hasEvidence)
		if err != nil {
			return ItemClaimResult{}, fmt.Errorf("load claim evidence: %w", err)
		}
		if !hasEvidence {
			return ItemClaimResult{}, ErrEvidenceRequired
		}

		if player.SceneID != "town" || player.Floor != 0 {
			return ItemClaimResult{}, ErrOutsideInvestigationRange
		}

		// 与 MissingCart 的位置和现有调查范围保持一致。
		distance := math.Hypot(
			player.Position.X-(-5),
			player.Position.Y-(-96),
		)
		if math.IsNaN(distance) || math.IsInf(distance, 0) ||
			distance > 24 {
			return ItemClaimResult{}, ErrOutsideInvestigationRange
		}

		// 先登记领取来源，唯一约束防止重复领取。
		result, err := tx.ExecContext(ctx, `
			INSERT INTO player_item_claims (
				world_id, player_id, source_id, item_id, quantity
			)
			VALUES (?, ?, ?, ?, 1)
			ON CONFLICT(world_id, player_id, source_id, item_id)
			DO NOTHING
		`, worldID, playerID, sourceID, itemID)
		if err != nil {
			return ItemClaimResult{}, fmt.Errorf("save item claim: %w", err)
		}

		affected, err := result.RowsAffected()
		if err != nil {
			return ItemClaimResult{}, fmt.Errorf("read item claim result: %w", err)
		}
		newlyClaimed = affected == 1

		// 只有首次登记成功，才给背包增加一份物品。
		if newlyClaimed {
			_, err = tx.ExecContext(ctx, `
				INSERT INTO player_inventory (
					world_id, player_id, item_id, quantity
				)
				VALUES (?, ?, ?, 1)
				ON CONFLICT(world_id, player_id, item_id)
				DO UPDATE SET
					quantity = player_inventory.quantity + excluded.quantity
			`, worldID, playerID, itemID)
			if err != nil {
				return ItemClaimResult{}, fmt.Errorf("add inventory item: %w", err)
			}
		}
	}

	// 返回当前持有数量；没有背包记录时按 0 处理。
	var quantity int64

	err = tx.QueryRowContext(ctx, `
		SELECT COALESCE((
			SELECT quantity
			FROM player_inventory
			WHERE world_id = ?
				AND player_id = ?
				AND item_id = ?
		), 0)
	`, worldID, playerID, itemID).Scan(&quantity)
	if err != nil {
		return ItemClaimResult{}, fmt.Errorf("load item quantity: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return ItemClaimResult{}, fmt.Errorf("commit item claim: %w", err)
	}

	response := ItemClaimResult{
		SourceID:     sourceID,
		ItemID:       itemID,
		NewlyClaimed: newlyClaimed,
		Quantity:     quantity,
		Message:      "你已经领取过这里的货物凭证。",
	}
	if newlyClaimed {
		response.Message = "获得商队货物凭证 × 1。"
	}

	return response, nil
}
