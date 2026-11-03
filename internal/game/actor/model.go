package actor

import "cybertown/internal/game/model"

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
