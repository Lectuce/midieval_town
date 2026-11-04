package npc

import (
	"context"
	"cybertown/internal/agent/engine"
	"cybertown/internal/game/actor"
	"fmt"

	"github.com/anthropics/anthropic-sdk-go"
)

type Agent struct {
	Engine *engine.Engine
}

func (a *Agent) Reply(
	ctx context.Context,
	character actor.NPC,
	player actor.Player,
	facts []actor.KnownFact,
	history []anthropic.MessageParam,
	req ChatRequest,
	onText func(string) error,
) (engine.TurnResult, error) {
	if a == nil || a.Engine == nil {
		return engine.TurnResult{}, fmt.Errorf("NPC engine is required")
	}

	// 当前约定：一个玩家与一个 NPC 对应一个持续的会话。
	conversationID := fmt.Sprintf(
		"%q/%q/%q",
		character.WorldID,
		player.ID,
		character.ID,
	)

	return a.Engine.RunTurn(ctx, engine.TurnRequest{
		Scope: engine.Scope{
			WorldID:   character.WorldID,
			AgentKind: "npc",
			AgentID:   character.ID,
			PlayerID:  player.ID,
			SessionID: conversationID,
			TurnID:    req.RequestID,
		},
		SystemPrompt: BuildSystemPrompt(character, player, facts),
		Messages:     history,
		Input:        req.Message,
		ToolSet:      "npc",
		OnText:       onText,
	})
}
