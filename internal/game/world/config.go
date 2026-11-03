package world

import (
	"encoding/json"
	"fmt"

	"os"
)

func LoadTownConfig(path string) (TownConfig, error) {

	data, err := os.ReadFile(path)
	if err != nil {
		return TownConfig{}, fmt.Errorf("read town config %v: %v", path, err)
	}

	var config TownConfig

	err = json.Unmarshal(data, &config)
	if err != nil {
		return TownConfig{}, fmt.Errorf("decode town config %v: %v", err, err)
	}

	err = ValidateTownConfig(config)
	if err != nil {
		return TownConfig{}, fmt.Errorf("validate town config %v: %v", path, err)
	}

	return config, nil

}

func ValidateTownConfig(config TownConfig) error {
	if config.World.ID == "" {
		return fmt.Errorf("world id is required")
	}

	if config.World.GameTime.IsZero() {
		return fmt.Errorf("world game time is required")
	}

	if config.World.Revision < 1 {
		return fmt.Errorf("world revision must be positive")
	}

	if len(config.Scenes) != 1 {
		return fmt.Errorf("one scene is required")
	}

	scene := config.Scenes[0]
	if scene.WorldID != config.World.ID {
		return fmt.Errorf("scene world id does not match world")
	}

	if scene.Name == "" {
		return fmt.Errorf("scene name is required")
	}

	if len(config.Players) != 1 {
		return fmt.Errorf(
			"exactly one player is required, got %v",
			len(config.Players),
		)
	}

	if len(config.NPCs) == 0 {
		return fmt.Errorf("at least one NPC is required")
	}

	player := config.Players[0]

	if player.ID == "" {
		return fmt.Errorf("player id is required")
	}

	if player.WorldID != config.World.ID {
		return fmt.Errorf(
			"player %v belongs to world %v, expected %vv",
			player.ID,
			player.WorldID,
			config.World.ID,
		)
	}

	if player.SceneID != scene.ID {
		return fmt.Errorf("player %q references unknown scene", player.ID)
	}

	if player.Status == "" {
		return fmt.Errorf(
			"player %v status is required",
			player.ID,
		)
	}

	if player.Revision < 1 {
		return fmt.Errorf(
			"player %v revision must be positive",
			player.ID,
		)
	}

	npcIDs := make(map[string]struct{}, len(config.NPCs))

	for _, npc := range config.NPCs {
		if npc.ID == "" {
			return fmt.Errorf("NPC id is required")
		}

		if _, exists := npcIDs[npc.ID]; exists {
			return fmt.Errorf(
				"duplicate NPC id: %v",
				npc.ID,
			)
		}

		npcIDs[npc.ID] = struct{}{}

		if npc.WorldID != config.World.ID {
			return fmt.Errorf(
				"NPC %v belongs to world %v, expected %v",
				npc.ID,
				npc.WorldID,
				config.World.ID,
			)
		}

		if npc.SceneID != scene.ID {
			return fmt.Errorf("player %v references unkown scene", npc.ID)
		}

		if npc.Name == "" {
			return fmt.Errorf(
				"NPC %v name is required",
				npc.ID,
			)
		}

		if npc.Role == "" {
			return fmt.Errorf(
				"NPC %v role is required",
				npc.ID,
			)
		}

		if npc.Persona == "" {
			return fmt.Errorf(
				"NPC %v persona is required",
				npc.ID,
			)
		}

		if npc.Background == "" {
			return fmt.Errorf(
				"NPC %v background is required",
				npc.ID,
			)
		}

		if npc.CurrentActivity == "" {
			return fmt.Errorf(
				"NPC %v current activity is required",
				npc.ID,
			)
		}

		if npc.Revision < 1 {
			return fmt.Errorf(
				"NPC %v revision must be positive",
				npc.ID,
			)
		}
	}

	return nil
}
