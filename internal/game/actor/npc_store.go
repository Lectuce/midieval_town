package actor

import (
	"context"
	"database/sql"
	"fmt"
)

func GetNPC(
	ctx context.Context,
	db *sql.DB,
	worldID string,
	npcID string,
) (NPC, error) {
	var npc NPC

	err := db.QueryRowContext(ctx,
		`
		SELECT
			world_id, id, name, role, persona, background,
			scene_id, current_activity,
			position_x, position_y, floor, revision
		FROM npcs
		WHERE world_id = ? AND id = ?
	`, worldID, npcID).Scan(
		&npc.WorldID,
		&npc.ID,
		&npc.Name,
		&npc.Role,
		&npc.Persona,
		&npc.Background,
		&npc.SceneID,
		&npc.CurrentActivity,
		&npc.Position.X,
		&npc.Position.Y,
		&npc.Floor,
		&npc.Revision,
	)

	if err != nil {
		return NPC{}, fmt.Errorf(
			"get npc %q in world %q: %w",
			npcID,
			worldID,
			err,
		)
	}

	return npc, nil
}

func GetNPCPositions(
	ctx context.Context,
	db *sql.DB,
	worldID string,
) ([]NPCPositionSnapshot, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT
			id, scene_id, floor,
			position_x, position_y, revision
		FROM npcs
		WHERE world_id = ?
		ORDER BY id
	`, worldID)
	if err != nil {
		return nil, fmt.Errorf("query NPC positions: %w", err)
	}
	defer rows.Close()

	result := make([]NPCPositionSnapshot, 0)

	for rows.Next() {
		var snapshot NPCPositionSnapshot

		if err := rows.Scan(
			&snapshot.ID,
			&snapshot.SceneID,
			&snapshot.Floor,
			&snapshot.Position.X,
			&snapshot.Position.Y,
			&snapshot.Revision,
		); err != nil {
			return nil, fmt.Errorf("scan NPC position: %w", err)
		}

		result = append(result, snapshot)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read NPC positions: %w", err)
	}

	return result, nil
}
