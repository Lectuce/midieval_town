package quest

type InvestigationResult struct {
	PointID    string `json:"point_id"`
	EvidenceID string `json:"evidence_id"`
	NewlyFound bool   `json:"newly_found"`
	Message    string `json:"message"`

	Cart      Cart       `json:"cart"`
	SiteCargo CargoStock `json:"site_cargo"`
}

type NPCQuestStatus struct {
	QuestID           string `json:"quest_id"`
	Title             string `json:"title"`
	Status            string `json:"status"`
	RequiredItemID    string `json:"required_item_id"`
	RequiredItemCount int64  `json:"required_item_count"`
	RewardItemID      string `json:"reward_item_id"`
	RewardItemCount   int64  `json:"reward_item_count"`
}

type QuestCompletionResult struct {
	Quest            PlayerQuest `json:"quest"`
	AlreadyCompleted bool        `json:"already_completed"`
	Message          string      `json:"message"`
}

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
