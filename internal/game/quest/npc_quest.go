package game

import (
	"context"
	"database/sql"
	"fmt"
)

type NPCQuestStatus struct {
	QuestID           string `json:"quest_id"`
	Title             string `json:"title"`
	Status            string `json:"status"`
	RequiredItemID    string `json:"required_item_id"`
	RequiredItemCount int64  `json:"required_item_count"`
	RewardItemID      string `json:"reward_item_id"`
	RewardItemCount   int64  `json:"reward_item_count"`
}

func GetNPCQuestStatus(
	ctx context.Context,
	db *sql.DB,
	worldID string,
	playerID string,
	npcID string,
) (NPCQuestStatus, error) {
	if worldID == "" || playerID == "" || npcID == "" {
		return NPCQuestStatus{}, fmt.Errorf(
			"world id, player id and NPC id are required",
		)
	}

	definition, err := GetQuestDefinition("missing_iron_cart")
	if err != nil {
		return NPCQuestStatus{}, err
	}

	// 只允许任务发布者查询自己的委托记录。
	if npcID != definition.GiverNPCID {
		return NPCQuestStatus{}, fmt.Errorf(
			"当前角色无权查询这项委托的记录",
		)
	}

	if _, err := GetNPC(ctx, db, worldID, npcID); err != nil {
		return NPCQuestStatus{}, fmt.Errorf("load NPC: %w", err)
	}

	result := NPCQuestStatus{
		QuestID:           definition.ID,
		Title:             definition.Title,
		RequiredItemID:    definition.RequiredItemID,
		RequiredItemCount: definition.RequiredItemCount,
		RewardItemID:      definition.RewardItemID,
		RewardItemCount:   definition.RewardItemCount,
	}

	// 只查询委托是否接取、是否完成。
	// 不读取玩家背包和调查证据。
	err = db.QueryRowContext(ctx, `
		SELECT COALESCE(q.status, 'not_started')
		FROM players p
		LEFT JOIN player_quests q
			ON q.world_id = p.world_id
			AND q.player_id = p.id
			AND q.quest_id = ?
		WHERE p.world_id = ? AND p.id = ?
	`,
		definition.ID,
		worldID,
		playerID,
	).Scan(&result.Status)
	if err != nil {
		return NPCQuestStatus{}, fmt.Errorf(
			"query NPC quest status: %w", err,
		)
	}

	return result, nil
}
