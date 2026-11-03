package game

import "fmt"

const (
	QuestStatusActive    = "active"
	QuestStatusCompleted = "completed"
)

// 任务规则
type QuestDefinition struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	GiverNPCID  string `json:"giver_npc_id"`

	RequiredEvidenceID string `json:"required_evidence_id"`
	RequiredItemID     string `json:"required_item_id"`
	RequiredItemCount  int64  `json:"required_item_count"`

	RewardItemID    string `json:"reward_item_id"`
	RewardItemCount int64  `json:"reward_item_count"`
}

// 玩家保存的任务进度。
type PlayerQuest struct {
	WorldID  string `json:"world_id"`
	PlayerID string `json:"player_id"`
	QuestID  string `json:"quest_id"`
	Status   string `json:"status"`
	Revision int64  `json:"revision"`
}

func GetQuestDefinition(questID string) (QuestDefinition, error) {

	switch questID {
	case "missing_iron_cart":

		return QuestDefinition{
			ID:          "missing_iron_cart",
			Title:       "寻找铁矿货车",
			Description: "调查失踪的铁矿货车，确认货车位置，并将商队货物凭证交给商人马丁。",
			GiverNPCID:  "merchant",

			RequiredEvidenceID: "cart_found",
			RequiredItemID:     "shipment_receipt",
			RequiredItemCount:  1,

			RewardItemID:    "coin",
			RewardItemCount: 10,
		}, nil

	default:
		return QuestDefinition{}, fmt.Errorf("unkonwn quest: %v", questID)
	}

}
