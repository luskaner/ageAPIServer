package models

import (
	"fmt"
)

// seedGames is the catalog of the spec's Game.gameTitleId enum. The names are
// the ones a live GET /api/v4/mods/Games returns, including the short form of
// the Retold title.
var seedGames = []Game{
	{GameTitleID: 1, GameTitleName: "Age of Empires DE"},
	{GameTitleID: 2, GameTitleName: "Age of Empires II DE"},
	{GameTitleID: 3, GameTitleName: "Age of Empires III DE"},
	{GameTitleID: 4, GameTitleName: "Age of Empires IV"},
	{GameTitleID: 1001, GameTitleName: "AoM: Retold"},
}

// seedModTypes is the tag catalog, per game. The upstream catalog is not
// shared: every title has its own list. Games 1, 2 and 4 are transcribed from a
// live GET /api/v4/mods/Tags. Games 3 and 1001 were not captured, so they fall
// back to placeholderModTypes; the report says Retold reuses the Age of
// Empires III list plus "Other".
var seedModTypes = map[int64][]string{
	1: {"Scenarios", "Campaign", "Other"},
	2: {
		"AI", "Campaign", "Data Mod", "Graphics", "Movie", "Music",
		"Random Maps", "Scenarios", "Sounds", "Speech", "Taunt", "Terrain",
		"Menu Background", "UI", "Text", "Other", "Return of Rome",
	},
	4: {"Crafted Map", "Generated Map", "Game Mode", "Tuning Pack", "Observer"},
}

// placeholderModTypes is the working catalog for the games whose tag list was
// never captured. The captures show `filter` values from 1 to 24 in use, but
// they carry no response bodies, so the names are a placeholder. The ids are
// stable, so replacing a name does not move any mod.
var placeholderModTypes = []string{
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
		for index, name := range modTypesFor(game.GameTitleID) {
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

// modTypesFor returns the tag names of a game, falling back to the placeholder
// catalog when the upstream list was never captured.
func modTypesFor(gameTitleID int64) []string {
	if names, ok := seedModTypes[gameTitleID]; ok {
		return names
	}
	if gameTitleID == 1001 {
		// Retold reuses the Age of Empires III list plus "Other".
		return append(append([]string{}, modTypesFor(3)...), "Other")
	}
	return placeholderModTypes
}

// modTypeID keeps the tag ids unique across games: game 2 owns 1..N, game 3
// owns 101..100+N, and so on. The captures only prove that low ids are in use
// for game 2, so the offset is a local convention, not an upstream fact.
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
