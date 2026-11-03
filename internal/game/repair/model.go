package game

import "time"

const (
	RepairJobStatusRunning   = "running"
	RepairJobStatusCompleted = "completed"
	RepairJobStatusCancelled = "cancelled"
)

type RepairJob struct {
	WorldID     string `json:"world_id"`
	ID          string `json:"id"`
	CartID      string `json:"cart_id"`
	WorkerNPCID string `json:"worker_npc_id"`

	Status string `json:"status"`

	// 两个时间都是游戏时间。
	StartedAt time.Time `json:"started_at"`
	FinishAt  time.Time `json:"finish_at"`

	Revision int64 `json:"revision"`
}
