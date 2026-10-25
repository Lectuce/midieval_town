package game

const (
	RepairProposalStatusProposed  = "proposed"
	RepairProposalStatusConfirmed = "confirmed"
	RepairProposalStatusExecuted  = "executed"
	RepairProposalStatusCancelled = "cancelled"
)

// 创建时只传入条款，不允许调用者传入“双方已经同意”。
type CreateRepairProposalRequest struct {
	ID              string
	PriceCoins      int64
	MaterialCount   int64
	DurationMinutes int64
}

type RepairProposal struct {
	WorldID string `json:"world_id"`
	ID      string `json:"id"`
	CartID  string `json:"cart_id"`

	PayerNPCID  string `json:"payer_npc_id"`
	WorkerNPCID string `json:"worker_npc_id"`

	PriceCoins      int64 `json:"price_coins"`
	MaterialCount   int64 `json:"material_count"`
	DurationMinutes int64 `json:"duration_minutes"`

	PayerConfirmed  bool `json:"payer_confirmed"`
	WorkerConfirmed bool `json:"worker_confirmed"`

	Status      string `json:"status"`
	RepairJobID string `json:"repair_job_id,omitempty"`
	Revision    int64  `json:"revision"`
}
