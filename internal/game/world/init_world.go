package world

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

func InitWorld(ctx context.Context, db *sql.DB, config TownConfig) error {

	err := ValidateTownConfig(config)
	if err != nil {
		return fmt.Errorf("validate initial world: %w", err)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin world initialization: %w", err)
	}
	defer tx.Rollback()

	// 尝试创建世界。已有同一世界时，不覆盖存档。
	result, err := tx.ExecContext(ctx, `
		INSERT INTO worlds (id, game_time, revision)
		VALUES (?, ?, ?)
		ON CONFLICT(id) DO NOTHING
	`,
		config.World.ID,
		config.World.GameTime.Format(time.RFC3339Nano),
		config.World.Revision,
	)

	if err != nil {
		return fmt.Errorf("insert world: %w", err)
	}

	inserted, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read world insert result: %w", err)
	}

	if inserted == 0 {
		// 世界已经存在，保留数据库里的状态。
		return nil
	}

	for _, scene := range config.Scenes {
		_, err := tx.ExecContext(ctx, `
			INSERT INTO scenes (world_id, id, name)
			VALUES (?, ?, ?)
		`,
			scene.WorldID,
			scene.ID,
			scene.Name,
		)
		if err != nil {
			return fmt.Errorf("insert scene %q: %w", scene.ID, err)
		}
	}

	for _, player := range config.Players {
		_, err := tx.ExecContext(ctx, `
			INSERT INTO players (
				world_id, id, name, scene_id, status,
				position_x, position_y, floor, revision
			)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		`,
			player.WorldID,
			player.ID,
			player.Name,
			player.SceneID,
			player.Status,
			player.Position.X,
			player.Position.Y,
			player.Floor,
			player.Revision,
		)
		if err != nil {
			return fmt.Errorf("insert player %q: %w", player.ID, err)
		}
	}

	for _, npc := range config.NPCs {
		_, err := tx.ExecContext(ctx, `
			INSERT INTO npcs (
				world_id, id, name, role, persona, background,
				scene_id, current_activity,
				position_x, position_y, floor, revision
			)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`,
			npc.WorldID,
			npc.ID,
			npc.Name,
			npc.Role,
			npc.Persona,
			npc.Background,
			npc.SceneID,
			npc.CurrentActivity,
			npc.Position.X,
			npc.Position.Y,
			npc.Floor,
			npc.Revision,
		)
		if err != nil {
			return fmt.Errorf("insert NPC %q: %w", npc.ID, err)
		}
	}

	err = tx.Commit()
	if err != nil {
		return fmt.Errorf("commit world initialization: %w", err)
	}

	return nil
}
