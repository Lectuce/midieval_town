package npc

import (
	"context"
	"cybertown/internal/agent/engine"
	"cybertown/internal/game"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
)

func BuildToolHandlers(db *sql.DB) map[string]engine.ToolHandler {

	handlers := make(map[string]engine.ToolHandler, len(engine.NPCToolHandlers)+1)

	for name, handler := range engine.NPCToolHandlers {
		handlers[name] = handler
	}

	handlers["get_my_quest_status"] = func(
		ctx context.Context,
		scope engine.Scope,
		input map[string]any,
	) (string, error) {
		return getMyQuestStatus(ctx, db, scope, input)
	}

	handlers["get_known_facts"] = func(
		ctx context.Context,
		scope engine.Scope,
		input map[string]any) (string, error) {
		return getKnownFacts(ctx, db, scope, input)
	}

	handlers["get_my_repair_resources"] = func(
		ctx context.Context,
		scope engine.Scope,
		input map[string]any,
	) (string, error) {
		return getMyRepairResources(ctx, db, scope, input)
	}

	handlers["propose_cart_repair"] = func(
		ctx context.Context,
		scope engine.Scope,
		input map[string]any,
	) (string, error) {
		return proposeCartRepair(ctx, db, scope, input)
	}

	handlers["get_received_repair_proposals"] = func(
		ctx context.Context,
		scope engine.Scope,
		input map[string]any,
	) (string, error) {
		return getReceivedRepairProposals(ctx, db, scope, input)
	}

	handlers["accept_repair_proposal"] = func(
		ctx context.Context,
		scope engine.Scope,
		input map[string]any,
	) (string, error) {
		return acceptRepairProposal(ctx, db, scope, input)
	}

	handlers["respond_to_repair_proposal"] = func(
		ctx context.Context,
		scope engine.Scope,
		input map[string]any,
	) (string, error) {
		return respondToRepairProposal(ctx, db, scope, input)
	}

	return handlers
}

func getMyQuestStatus(
	ctx context.Context,
	db *sql.DB,
	scope engine.Scope,
	input map[string]any,
) (string, error) {
	if scope.AgentKind != "npc" {
		return "", fmt.Errorf("this tool requires an NPC scope")
	}

	status, err := game.GetNPCQuestStatus(
		ctx,
		db,
		scope.WorldID,
		scope.PlayerID,
		scope.AgentID,
	)
	if err != nil {
		return "", err
	}

	raw, err := json.Marshal(status)
	if err != nil {
		return "", fmt.Errorf("encode quest status: %w", err)
	}

	return string(raw), nil
}

func getKnownFacts(
	ctx context.Context,
	db *sql.DB,
	scope engine.Scope,
	input map[string]any,
) (string, error) {
	if scope.AgentKind != "npc" {
		return "", fmt.Errorf("this tool requires an NPC scope")
	}

	status, err := game.GetKnownFacts(
		ctx,
		db,
		scope.WorldID,
		scope.AgentID,
	)
	if err != nil {
		return "", err
	}

	raw, err := json.Marshal(status)
	if err != nil {
		return "", fmt.Errorf("encode quest status: %w", err)
	}

	log.Printf(
		"NPC knowledge turn=%s npc=%s result=%s",
		scope.TurnID,
		scope.AgentID,
		string(raw),
	)

	return string(raw), nil

}
