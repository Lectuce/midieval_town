package game

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
)

var (
	ErrPlayerInactive          = errors.New("player is not active")
	ErrQuestItemsRequired      = errors.New("required quest items are missing")
	ErrOutsideQuestRange       = errors.New("player is outside quest giver range")
	ErrQuestCompletionConflict = errors.New("quest completion conflict")
)

type QuestCompletionResult struct {
	Quest            PlayerQuest `json:"quest"`
	AlreadyCompleted bool        `json:"already_completed"`
	Message          string      `json:"message"`
}

func AcceptQuest(ctx context.Context, db *sql.DB, worldID string, playerID string, questID string) (PlayerQuest, error) {
	if worldID == "" || playerID == "" || questID == "" {
		return PlayerQuest{}, fmt.Errorf(
			"world id, player id and quest id are required",
		)
	}

	definition, err := GetQuestDefinition(questID)
	if err != nil {
		return PlayerQuest{}, err
	}

	player, err := GetPlayer(ctx, db, worldID, playerID)
	if err != nil {
		return PlayerQuest{}, err
	}

	if player.Status != "active" {
		return PlayerQuest{}, ErrPlayerInactive
	}

	// 确认这个世界中确实存在任务发布者。
	_, err = GetNPC(ctx, db, worldID, definition.GiverNPCID)
	if err != nil {
		return PlayerQuest{}, fmt.Errorf("load quest giver: %w", err)
	}

	_, err = db.ExecContext(ctx, `
		INSERT INTO player_quests (
			world_id, player_id, quest_id, status, revision
		)
		VALUES (?, ?, ?, 'active', 1)
		ON CONFLICT(world_id, player_id, quest_id) DO NOTHING
	`,
		worldID,
		playerID,
		questID,
	)
	if err != nil {
		return PlayerQuest{}, fmt.Errorf("accept quest: %w", err)
	}

	return GetPlayerQuest(
		ctx,
		db,
		worldID,
		playerID,
		questID,
	)
}

func CompleteQuest(
	ctx context.Context,
	db *sql.DB,
	worldID string,
	playerID string,
	questID string,
) (QuestCompletionResult, error) {
	if worldID == "" || playerID == "" || questID == "" {
		return QuestCompletionResult{}, fmt.Errorf(
			"world id, player id and quest id are required",
		)
	}

	definition, err := GetQuestDefinition(questID)
	if err != nil {
		return QuestCompletionResult{}, err
	}

	// 当前任务要求提交物品，并给予物品奖励。
	if definition.RequiredItemCount <= 0 ||
		definition.RewardItemCount <= 0 {
		return QuestCompletionResult{}, fmt.Errorf(
			"invalid quest item counts",
		)
	}

	if _, err := GetItemDefinition(definition.RequiredItemID); err != nil {
		return QuestCompletionResult{}, err
	}
	if _, err := GetItemDefinition(definition.RewardItemID); err != nil {
		return QuestCompletionResult{}, err
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return QuestCompletionResult{}, fmt.Errorf(
			"begin quest completion: %w", err,
		)
	}
	defer tx.Rollback()

	// 读取玩家。
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
		return QuestCompletionResult{}, fmt.Errorf(
			"load completion player: %w", err,
		)
	}

	// 读取玩家的任务进度。
	quest := PlayerQuest{
		WorldID:  worldID,
		PlayerID: playerID,
		QuestID:  questID,
	}

	err = tx.QueryRowContext(ctx, `
		SELECT status, revision
		FROM player_quests
		WHERE world_id = ?
			AND player_id = ?
			AND quest_id = ?
	`, worldID, playerID, questID).Scan(
		&quest.Status,
		&quest.Revision,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return QuestCompletionResult{}, ErrQuestNotActive
	}
	if err != nil {
		return QuestCompletionResult{}, fmt.Errorf(
			"load completion quest: %w", err,
		)
	}

	// 已经完成，只确认历史结果，不再次扣物品或发奖励。
	if quest.Status == QuestStatusCompleted {
		return QuestCompletionResult{
			Quest:            quest,
			AlreadyCompleted: true,
			Message:          "这项委托已经完成，奖励已结算。",
		}, nil
	}

	if quest.Status != QuestStatusActive {
		return QuestCompletionResult{}, ErrQuestNotActive
	}
	if player.Status != "active" {
		return QuestCompletionResult{}, ErrPlayerInactive
	}

	// 读取任务发布者的当前位置。
	var giver NPC

	err = tx.QueryRowContext(ctx, `
		SELECT scene_id, position_x, position_y, floor
		FROM npcs
		WHERE world_id = ? AND id = ?
	`, worldID, definition.GiverNPCID).Scan(
		&giver.SceneID,
		&giver.Position.X,
		&giver.Position.Y,
		&giver.Floor,
	)
	if err != nil {
		return QuestCompletionResult{}, fmt.Errorf(
			"load quest giver: %w", err,
		)
	}

	if player.SceneID != giver.SceneID ||
		player.Floor != giver.Floor {
		return QuestCompletionResult{}, ErrOutsideQuestRange
	}

	distance := math.Hypot(
		player.Position.X-giver.Position.X,
		player.Position.Y-giver.Position.Y,
	)
	if math.IsNaN(distance) || math.IsInf(distance, 0) ||
		distance > 24 {
		return QuestCompletionResult{}, ErrOutsideQuestRange
	}

	// 检查任务要求的证据。
	var hasEvidence bool

	err = tx.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM player_evidence
			WHERE world_id = ?
				AND player_id = ?
				AND evidence_id = ?
		)
	`,
		worldID,
		playerID,
		definition.RequiredEvidenceID,
	).Scan(&hasEvidence)
	if err != nil {
		return QuestCompletionResult{}, fmt.Errorf(
			"load completion evidence: %w", err,
		)
	}
	if !hasEvidence {
		return QuestCompletionResult{}, ErrEvidenceRequired
	}

	// 条件扣除物品：只有数量足够时才更新。
	result, err := tx.ExecContext(ctx, `
		UPDATE player_inventory
		SET quantity = quantity - ?
		WHERE world_id = ?
			AND player_id = ?
			AND item_id = ?
			AND quantity >= ?
	`,
		definition.RequiredItemCount,
		worldID,
		playerID,
		definition.RequiredItemID,
		definition.RequiredItemCount,
	)
	if err != nil {
		return QuestCompletionResult{}, fmt.Errorf(
			"consume quest item: %w", err,
		)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return QuestCompletionResult{}, fmt.Errorf(
			"read consumed item result: %w", err,
		)
	}
	if affected != 1 {
		return QuestCompletionResult{}, ErrQuestItemsRequired
	}

	// 只有仍处于 active 且版本一致的任务才能完成。
	result, err = tx.ExecContext(ctx, `
		UPDATE player_quests
		SET status = 'completed',
			revision = revision + 1
		WHERE world_id = ?
			AND player_id = ?
			AND quest_id = ?
			AND status = 'active'
			AND revision = ?
	`,
		worldID,
		playerID,
		questID,
		quest.Revision,
	)
	if err != nil {
		return QuestCompletionResult{}, fmt.Errorf(
			"update completed quest: %w", err,
		)
	}

	affected, err = result.RowsAffected()
	if err != nil {
		return QuestCompletionResult{}, fmt.Errorf(
			"read quest update result: %w", err,
		)
	}
	if affected != 1 {
		return QuestCompletionResult{}, ErrQuestCompletionConflict
	}

	// 发放任务定义中的奖励。
	_, err = tx.ExecContext(ctx, `
		INSERT INTO player_inventory (
			world_id, player_id, item_id, quantity
		)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(world_id, player_id, item_id)
		DO UPDATE SET
			quantity = player_inventory.quantity + excluded.quantity
	`,
		worldID,
		playerID,
		definition.RewardItemID,
		definition.RewardItemCount,
	)
	if err != nil {
		return QuestCompletionResult{}, fmt.Errorf(
			"grant quest reward: %w", err,
		)
	}

	// 提交成功后，才向调用方报告完成。
	if err := tx.Commit(); err != nil {
		return QuestCompletionResult{}, fmt.Errorf(
			"commit quest completion: %w", err,
		)
	}

	quest.Status = QuestStatusCompleted
	quest.Revision++

	return QuestCompletionResult{
		Quest:            quest,
		AlreadyCompleted: false,
		Message:          "委托已完成，奖励已放入背包。",
	}, nil
}
