package world

import (
	"cybertown/internal/game/actor"
	"time"
)

type Scene struct {
	WorldID string `json:"world_id"`
	ID      string `json:"id"`
	Name    string `json:"name"`
}

type World struct {
	ID       string    `json:"id"`
	GameTime time.Time `json:"game_time"`

	// 世界状态版本号，世界状态发生重要变化时递增，用于判断 Agent 是否基于过期状态进行了规划
	Revision int64 `json:"revision"`
}

type TownConfig struct {
	World   World          `json:"world"`
	Scenes  []Scene        `json:"scenes"`
	Players []actor.Player `json:"players"`
	NPCs    []actor.NPC    `json:"npcs"`
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
