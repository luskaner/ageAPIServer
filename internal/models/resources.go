package models

import (
	"fmt"
)

// seedGames is the catalog of the spec's Game.gameTitleId enum.
var seedGames = []Game{
	{GameTitleID: 1, GameTitleName: "Age of Empires DE"},
	{GameTitleID: 2, GameTitleName: "Age of Empires II DE"},
	{GameTitleID: 3, GameTitleName: "Age of Empires III DE"},
	{GameTitleID: 4, GameTitleName: "Age of Empires IV"},
	{GameTitleID: 1001, GameTitleName: "Age of Mythology: Retold"},
}

// seedModTypeNames is the working tag catalog. The captures show `filter`
// values from 1 to 24 in use, but they carry no response bodies, so the names
// are a placeholder to be replaced by the real catalog. The ids are stable, so
// replacing a name does not move any mod.
var seedModTypeNames = []string{
	"Random Maps",
	"Campaign",
	"Data Mod",
	"AI",
	"User Interface",
	"Audio",
	"Graphics",
	"Scenario",
	"Building Set",
	"Unit",
	"Civilisation",
	"Terrain",
	"Textures",
	"Localisation",
	"Balance",
	"Quality of Life",
	"Hotkey",
	"Overlay",
	"Game Mode",
	"Map Pack",
	"Skin",
	"Utility",
	"Total Conversion",
	"Other",
}

// Seed inserts the catalogs. It is idempotent: existing rows are kept.
func (s *SystemStore) Seed() error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin seed: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	for _, game := range seedGames {
		if _, err := tx.Exec(
			`INSERT OR IGNORE INTO games (gameTitleId, gameTitleName, gameVersion) VALUES (?, ?, NULL)`,
			game.GameTitleID, game.GameTitleName); err != nil {
			return fmt.Errorf("seed game %d: %w", game.GameTitleID, err)
		}
	}
	for _, game := range seedGames {
		for index, name := range seedModTypeNames {
			if _, err := tx.Exec(
				`INSERT OR IGNORE INTO mod_types (modTypeId, modTypeName, gameTitleId) VALUES (?, ?, ?)`,
				modTypeID(game.GameTitleID, index+1), name, game.GameTitleID); err != nil {
				return fmt.Errorf("seed mod type %d/%d: %w", game.GameTitleID, index+1, err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit seed: %w", err)
	}
	return nil
}

// modTypeID keeps the tag ids unique across games: game 2 owns 1..24, game 3
// owns 101..124, and so on. The captures only prove that low ids are in use for
// game 2, so the offset is a local convention, not an upstream fact.
func modTypeID(gameTitleID int64, index int) int64 {
	switch gameTitleID {
	case 2:
		return int64(index)
	case 3:
		return int64(100 + index)
	case 4:
		return int64(200 + index)
	case 1001:
		return int64(300 + index)
	default:
		return int64(400 + index)
	}
}
