package quest

import "fmt"

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
