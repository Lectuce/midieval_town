package actor

import "cybertown/internal/game/model"

type Player struct {
	WorldID  string         `json:"world_id"`
	ID       string         `json:"id"`
	Name     string         `json:"name"`
	SceneID  string         `json:"scene_id"`
	Status   string         `json:"status"`
	Revision int64          `json:"revision"`
	Position model.Position `json:"position"`
	Floor    int            `json:"floor"`
}

type NPC struct {
	WorldID string `json:"world_id"`
	ID      string `json:"id"`
	Name    string `json:"name"`

	// 职业
	Role string `json:"role"`

	// 性格和说法方式
	Persona    string `json:"persona"`
	Background string `json:"background"`

	// NPC当前所在场景
	SceneID         string `json:"scene_id"`
	CurrentActivity string `json:"current_activity"`

	// NPC状态版本
	Revision int64 `json:"revision"`

	Position model.Position `json:"position"`
	Floor    int            `json:"floor"`
}

type KnownFact struct {
	ID      string `json:"id"`
	Content string `json:"content"`
	Source  string `json:"source"`
}

// 只包含客户端显示所需的数据，不返回角色私人设定。
type NPCPositionSnapshot struct {
	ID       string         `json:"id"`
	SceneID  string         `json:"scene_id"`
	Floor    int            `json:"floor"`
	Position model.Position `json:"position"`
	Revision int64          `json:"revision"`
}
