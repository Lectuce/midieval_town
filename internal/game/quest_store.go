package game

import (
	"context"
	"database/sql"
	"fmt"
)

func GetPlayerQuest(ctx context.Context, db *sql.DB, worldID string, playerID string, questID string) (PlayerQuest, error) {
	var quest PlayerQuest

	err := db.QueryRowContext(ctx, `
		SELECT world_id, player_id, quest_id, status, revision
		FROM player_quests
		WHERE world_id = ?
			AND player_id = ?
			AND quest_id = ?
	`,
		worldID,
		playerID,
		questID,
	).Scan(
		&quest.WorldID,
		&quest.PlayerID,
		&quest.QuestID,
		&quest.Status,
		&quest.Revision,
	)
	if err != nil {
		return PlayerQuest{}, fmt.Errorf("get player quest %q: %w", questID, err)
	}

	return quest, nil
}
