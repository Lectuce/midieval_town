package model

import "time"

// 玩家在哪，NPC在哪，哪些场景相邻

type World struct {
	ID       string    `json:"id"`
	GameTime time.Time `json:"game_time"`

	// 世界状态版本号，世界状态发生重要变化时递增，用于判断 Agent 是否基于过期状态进行了规划
	Revision int64 `json:"revision"`
}

type Scene struct {
	WorldID string `json:"world_id"`
	ID      string `json:"id"`
	Name    string `json:"name"`
}

type Player struct {
	WorldID  string   `json:"world_id"`
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	SceneID  string   `json:"scene_id"`
	Status   string   `json:"status"`
	Revision int64    `json:"revision"`
	Position Position `json:"position"`
	Floor    int      `json:"floor"`
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

	Position Position `json:"position"`
	Floor    int      `json:"floor"`
}

type TownConfig struct {
	World   World    `json:"world"`
	Scenes  []Scene  `json:"scenes"`
	Players []Player `json:"players"`
	NPCs    []NPC    `json:"npcs"`
}

// 角色初始坐标
type Position struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

type Location struct {
	ID      string `json:"id"`
	SceneID string `json:"scene_id"`
	Name    string `json:"name"`

	MinX int `json:"min_x"`
	MinY int `json:"min_y"`
	MaxX int `json:"max_x"`
	MaxY int `json:"max_y"`
}
