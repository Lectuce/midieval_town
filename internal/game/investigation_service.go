package game

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
)

var (
	ErrQuestNotActive            = errors.New("quest is not active")
	ErrOutsideInvestigationRange = errors.New("player is outside investigation range")
	ErrTransportStateMissing     = errors.New("transport state is missing")
)

type InvestigationResult struct {
	PointID    string `json:"point_id"`
	EvidenceID string `json:"evidence_id"`
	NewlyFound bool   `json:"newly_found"`
	Message    string `json:"message"`

	Cart      Cart       `json:"cart"`
	SiteCargo CargoStock `json:"site_cargo"`
}

func InspectAbandonedCart(
	ctx context.Context,
	db *sql.DB,
	worldID string,
	playerID string,
) (InvestigationResult, error) {
	if worldID == "" || playerID == "" {
		return InvestigationResult{}, fmt.Errorf(
			"world id and player id are required",
		)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return InvestigationResult{}, fmt.Errorf(
			"begin investigation: %w", err,
		)
	}
	defer tx.Rollback()

	// 在事务内读取玩家，后续查询和写入也使用 tx。
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
		return InvestigationResult{}, fmt.Errorf(
			"load investigation player: %w", err,
		)
	}

	if player.Status != "active" {
		return InvestigationResult{}, ErrPlayerInactive
	}

	// 必须已经接受货车任务，而且任务仍在进行中。
	var questStatus string

	err = tx.QueryRowContext(ctx, `
		SELECT status
		FROM player_quests
		WHERE world_id = ?
			AND player_id = ?
			AND quest_id = ?
	`, worldID, playerID, "missing_iron_cart").Scan(&questStatus)

	if errors.Is(err, sql.ErrNoRows) {
		return InvestigationResult{}, ErrQuestNotActive
	}
	if err != nil {
		return InvestigationResult{}, fmt.Errorf(
			"load investigation quest: %w", err,
		)
	}
	if questStatus != QuestStatusActive {
		return InvestigationResult{}, ErrQuestNotActive
	}

	// 在当前事务中读取货车的真实状态。
	var cart Cart

	err = tx.QueryRowContext(ctx, `
		SELECT
			world_id, id, scene_id,
			position_x, position_y, floor,
			repair_status, capacity_kg, revision
		FROM carts
		WHERE world_id = ? AND id = ?
	`, worldID, AbandonedCartID).Scan(
		&cart.WorldID,
		&cart.ID,
		&cart.SceneID,
		&cart.Position.X,
		&cart.Position.Y,
		&cart.Floor,
		&cart.RepairStatus,
		&cart.CapacityKg,
		&cart.Revision,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return InvestigationResult{}, fmt.Errorf(
			"%w: cart %q",
			ErrTransportStateMissing,
			AbandonedCartID,
		)
	}
	if err != nil {
		return InvestigationResult{}, fmt.Errorf(
			"load investigation cart: %w",
			err,
		)
	}

	const interactionDistance = 24.0

	if player.SceneID != cart.SceneID || player.Floor != cart.Floor {
		return InvestigationResult{}, ErrOutsideInvestigationRange
	}

	distance := math.Hypot(
		player.Position.X-cart.Position.X,
		player.Position.Y-cart.Position.Y,
	)
	if math.IsNaN(distance) || math.IsInf(distance, 0) ||
		distance > interactionDistance {
		return InvestigationResult{}, ErrOutsideInvestigationRange
	}

	// 玩家到达交互范围后，读取现场铁矿数量。
	var stock CargoStock

	err = tx.QueryRowContext(ctx, `
		SELECT
			world_id, location_id, item_id,
			quantity_kg, revision
		FROM cargo_stocks
		WHERE world_id = ?
			AND location_id = ?
			AND item_id = ?
	`,
		worldID,
		CargoLocationCartSite,
		IronOreItemID,
	).Scan(
		&stock.WorldID,
		&stock.LocationID,
		&stock.ItemID,
		&stock.QuantityKg,
		&stock.Revision,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return InvestigationResult{}, fmt.Errorf(
			"%w: cargo stock at %q",
			ErrTransportStateMissing,
			CargoLocationCartSite,
		)
	}
	if err != nil {
		return InvestigationResult{}, fmt.Errorf(
			"load investigation cargo: %w",
			err,
		)
	}

	// 同一证据只保存一次，重复调查不覆盖首次发现记录。
	result, err := tx.ExecContext(ctx, `
		INSERT INTO player_evidence (
			world_id, player_id, evidence_id, source_id
		)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(world_id, player_id, evidence_id) DO NOTHING
	`,
		worldID,
		playerID,
		"cart_found",
		"abandoned_cart",
	)
	if err != nil {
		return InvestigationResult{}, fmt.Errorf(
			"save investigation evidence: %w", err,
		)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return InvestigationResult{}, fmt.Errorf(
			"read evidence insert result: %w", err,
		)
	}

	if err := tx.Commit(); err != nil {
		return InvestigationResult{}, fmt.Errorf(
			"commit investigation: %w", err,
		)
	}

	var repairDescription string

	switch cart.RepairStatus {
	case CartStatusDamaged:
		repairDescription = "货车已经损坏。"
	case CartStatusRepairing:
		repairDescription = "货车正在维修。"
	case CartStatusReady:
		repairDescription = "货车可以使用。"
	default:
		repairDescription = "货车状态未知。"
	}

	response := InvestigationResult{
		PointID:    AbandonedCartID,
		EvidenceID: "cart_found",
		NewlyFound: affected == 1,
		Message:    "已确认失踪货车的位置。",
		Cart:       cart,
		SiteCargo:  stock,
	}

	if !response.NewlyFound {
		response.Message = "你再次查看了货车现场。"
	}

	response.Message += fmt.Sprintf(
		"%s现场留有 %d 千克铁矿。",
		repairDescription,
		stock.QuantityKg,
	)

	return response, nil
}
