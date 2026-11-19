// SPDX-License-Identifier: AGPL-3.0-only

package models

import (
	"fmt"
)

// seedGames is the catalog of the spec's Game.gameTitleId enum. It is the exact
// body of a live GET /api/v4/mods/Games, including the short Retold title.
var seedGames = []Game{
	{GameTitleID: 1, GameTitleName: "Age of Empires DE"},
	{GameTitleID: 2, GameTitleName: "Age of Empires II DE"},
	{GameTitleID: 3, GameTitleName: "Age of Empires III DE"},
	{GameTitleID: 4, GameTitleName: "Age of Empires IV"},
	{GameTitleID: 1001, GameTitleName: "AoM: Retold"},
}

// seedModTypes is the exact body of a live GET /api/v4/mods/Tags: 56 rows over
// 22 distinct modTypeId values. The id is global, not per game, so the same id
// means different things in different titles: 1 is "Single-player scenarios" in
// game 1 and "Crafted Map" in game 4. The pair (gameTitleId, modTypeId) is the
// key, which is why mod_types is keyed on both.
//
// The slice order is the order the upstream returns, and Seed records it in
// mod_types.sortOrder so the endpoint reproduces the response byte for byte.
var seedModTypes = []ModTag{
	{ModTypeID: 1, ModTypeName: "Single-player scenarios", GameTitleID: 1},
	{ModTypeID: 2, ModTypeName: "Multiplayer scenarios", GameTitleID: 1},
	{ModTypeID: 10, ModTypeName: "Campaign", GameTitleID: 1},
	{ModTypeID: 9, ModTypeName: "Artificial Intelligence", GameTitleID: 2},
	{ModTypeID: 10, ModTypeName: "Campaign", GameTitleID: 2},
	{ModTypeID: 11, ModTypeName: "Data Mod", GameTitleID: 2},
	{ModTypeID: 12, ModTypeName: "Graphics", GameTitleID: 2},
	{ModTypeID: 13, ModTypeName: "Movie", GameTitleID: 2},
	{ModTypeID: 14, ModTypeName: "Music", GameTitleID: 2},
	{ModTypeID: 15, ModTypeName: "Random Maps", GameTitleID: 2},
	{ModTypeID: 16, ModTypeName: "Scenarios", GameTitleID: 2},
	{ModTypeID: 17, ModTypeName: "Sounds", GameTitleID: 2},
	{ModTypeID: 18, ModTypeName: "Speech", GameTitleID: 2},
	{ModTypeID: 19, ModTypeName: "Taunt", GameTitleID: 2},
	{ModTypeID: 20, ModTypeName: "Terrain", GameTitleID: 2},
	{ModTypeID: 21, ModTypeName: "Menu Background", GameTitleID: 2},
	{ModTypeID: 22, ModTypeName: "User Interface", GameTitleID: 2},
	{ModTypeID: 23, ModTypeName: "Text", GameTitleID: 2},
	{ModTypeID: 9, ModTypeName: "Artificial Intelligence", GameTitleID: 3},
	{ModTypeID: 10, ModTypeName: "Campaign", GameTitleID: 3},
	{ModTypeID: 11, ModTypeName: "Data Mod", GameTitleID: 3},
	{ModTypeID: 12, ModTypeName: "Graphics", GameTitleID: 3},
	{ModTypeID: 13, ModTypeName: "Movie", GameTitleID: 3},
	{ModTypeID: 14, ModTypeName: "Music", GameTitleID: 3},
	{ModTypeID: 15, ModTypeName: "Random Maps", GameTitleID: 3},
	{ModTypeID: 16, ModTypeName: "Scenarios", GameTitleID: 3},
	{ModTypeID: 17, ModTypeName: "Sounds", GameTitleID: 3},
	{ModTypeID: 18, ModTypeName: "Speech", GameTitleID: 3},
	{ModTypeID: 19, ModTypeName: "Taunt", GameTitleID: 3},
	{ModTypeID: 20, ModTypeName: "Terrain", GameTitleID: 3},
	{ModTypeID: 22, ModTypeName: "User Interface", GameTitleID: 3},
	{ModTypeID: 23, ModTypeName: "Text", GameTitleID: 3},
	{ModTypeID: 26, ModTypeName: "Other", GameTitleID: 2},
	{ModTypeID: 26, ModTypeName: "Other", GameTitleID: 3},
	{ModTypeID: 26, ModTypeName: "Other", GameTitleID: 1},
	{ModTypeID: 1, ModTypeName: "Crafted Map", GameTitleID: 4},
	{ModTypeID: 2, ModTypeName: "Generated Map", GameTitleID: 4},
	{ModTypeID: 3, ModTypeName: "Game Mode", GameTitleID: 4},
	{ModTypeID: 4, ModTypeName: "Tuning Pack", GameTitleID: 4},
	{ModTypeID: 5, ModTypeName: "Observer", GameTitleID: 4},
	{ModTypeID: 30, ModTypeName: "Return of Rome", GameTitleID: 2},
	{ModTypeID: 9, ModTypeName: "Artificial Intelligence", GameTitleID: 1001},
	{ModTypeID: 10, ModTypeName: "Campaign", GameTitleID: 1001},
	{ModTypeID: 11, ModTypeName: "Data Mod", GameTitleID: 1001},
	{ModTypeID: 12, ModTypeName: "Graphics", GameTitleID: 1001},
	{ModTypeID: 13, ModTypeName: "Movie", GameTitleID: 1001},
	{ModTypeID: 14, ModTypeName: "Music", GameTitleID: 1001},
	{ModTypeID: 15, ModTypeName: "Random Maps", GameTitleID: 1001},
	{ModTypeID: 16, ModTypeName: "Scenarios", GameTitleID: 1001},
	{ModTypeID: 17, ModTypeName: "Sounds", GameTitleID: 1001},
	{ModTypeID: 18, ModTypeName: "Speech", GameTitleID: 1001},
	{ModTypeID: 19, ModTypeName: "Taunt", GameTitleID: 1001},
	{ModTypeID: 20, ModTypeName: "Terrain", GameTitleID: 1001},
	{ModTypeID: 22, ModTypeName: "User Interface", GameTitleID: 1001},
	{ModTypeID: 23, ModTypeName: "Text", GameTitleID: 1001},
	{ModTypeID: 26, ModTypeName: "Other", GameTitleID: 1001},
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
	for index, tag := range seedModTypes {
		if _, err := tx.Exec(
			`INSERT OR IGNORE INTO mod_types (modTypeId, modTypeName, gameTitleId, sortOrder) VALUES (?, ?, ?, ?)`,
			tag.ModTypeID, tag.ModTypeName, tag.GameTitleID, index); err != nil {
			return fmt.Errorf("seed mod type %d/%d: %w", tag.GameTitleID, tag.ModTypeID, err)
		}
	}

	// A database created before the catalog became per game keeps the old
	// mod_types primary key, so the inserts above are silently dropped. Fail
	// loudly instead of serving a partial catalog.
	var seeded int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM mod_types`).Scan(&seeded); err != nil {
		return fmt.Errorf("count mod types: %w", err)
	}
	if seeded != len(seedModTypes) {
		return fmt.Errorf(
			"mod_types holds %d rows, want %d: the database predates the per-game catalog, delete it and start again",
			seeded, len(seedModTypes))
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit seed: %w", err)
	}
	return nil
}
