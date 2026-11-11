package models

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// schemaVersion is bumped whenever schemaSQL changes. Migrate applies the whole
// script with CREATE TABLE IF NOT EXISTS, so an existing database only gains
// the missing objects; a change to an existing table needs the file recreated.
const schemaVersion = 2

// SystemStore owns the schema and the health check.
type SystemStore struct {
	db *sql.DB
}

// schemaSQL is the full schema. It is idempotent by construction.
const schemaSQL = `
CREATE TABLE IF NOT EXISTS _migrations (
  version    INTEGER PRIMARY KEY,
  appliedAt  TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS games (
  gameTitleId    INTEGER PRIMARY KEY,
  gameTitleName  TEXT NOT NULL,
  gameVersion    TEXT
);

CREATE TABLE IF NOT EXISTS mod_types (
  modTypeId      INTEGER NOT NULL,
  modTypeName    TEXT NOT NULL,
  gameTitleId    INTEGER NOT NULL REFERENCES games(gameTitleId),
  sortOrder      INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (gameTitleId, modTypeId),
  UNIQUE (gameTitleId, modTypeName)
);

CREATE TABLE IF NOT EXISTS users (
  userId         INTEGER PRIMARY KEY AUTOINCREMENT,
  steamId        TEXT UNIQUE,
  displayName    TEXT,
  avatarUrl      TEXT,
  createdAt      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now'))
);

CREATE TABLE IF NOT EXISTS sessions (
  sessionId      TEXT PRIMARY KEY,
  userId         INTEGER NOT NULL REFERENCES users(userId),
  token          TEXT UNIQUE NOT NULL,
  createdAt      TEXT NOT NULL,
  expiresAt      TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_sessions_token ON sessions(token);
CREATE INDEX IF NOT EXISTS idx_sessions_expires ON sessions(expiresAt);

CREATE TABLE IF NOT EXISTS mods (
  modId             INTEGER PRIMARY KEY,
  gameTitleId       INTEGER NOT NULL REFERENCES games(gameTitleId),
  modName           TEXT NOT NULL,
  description       TEXT,
  modDescription    TEXT,
  changeList        TEXT,
  modVersion        REAL NOT NULL DEFAULT 0,
  verifiedVersion   TEXT,
  modTypeId         INTEGER,
  clientId          TEXT,
  modStatusId       INTEGER NOT NULL CHECK (modStatusId BETWEEN 1 AND 4),
  modStatusMessage  TEXT,
  modVisibility     TEXT NOT NULL DEFAULT 'Public'
                    CHECK (modVisibility IN ('Public','Private','Unlisted')),
  creatorId         INTEGER REFERENCES users(userId),
  createDate        TEXT NOT NULL,
  lastUpdate        TEXT NOT NULL,
  updateAvailable   INTEGER NOT NULL DEFAULT 0,
  popular           INTEGER NOT NULL DEFAULT 0,
  thirtyDayPopular  INTEGER NOT NULL DEFAULT 0,
  downloads         INTEGER NOT NULL DEFAULT 0,
  installs          INTEGER NOT NULL DEFAULT 0,
  likes             INTEGER NOT NULL DEFAULT 0,
  sortName          TEXT,
  sendNotifications INTEGER NOT NULL DEFAULT 0,
  allowDiscussion   INTEGER NOT NULL DEFAULT 0,
  discourseUrl      TEXT,
  thumbnail         TEXT,
  fileUrl           TEXT,
  fileCRC           TEXT,
  modFileSize       REAL,
  modDiskSize       REAL,
  metaData          TEXT,
  currentRating     REAL NOT NULL DEFAULT 0,
  positiveRatings   INTEGER NOT NULL DEFAULT 0,
  totalRatings      INTEGER NOT NULL DEFAULT 0,
  reviewsCount      INTEGER NOT NULL DEFAULT 0,
  FOREIGN KEY (gameTitleId, modTypeId) REFERENCES mod_types(gameTitleId, modTypeId)
);
CREATE INDEX IF NOT EXISTS idx_mods_game     ON mods(gameTitleId);
CREATE INDEX IF NOT EXISTS idx_mods_status   ON mods(modStatusId);
CREATE INDEX IF NOT EXISTS idx_mods_type     ON mods(modTypeId);
CREATE INDEX IF NOT EXISTS idx_mods_creator  ON mods(creatorId);
CREATE INDEX IF NOT EXISTS idx_mods_sortname ON mods(sortName);
CREATE INDEX IF NOT EXISTS idx_mods_popular  ON mods(gameTitleId, popular DESC);
CREATE INDEX IF NOT EXISTS idx_mods_recent   ON mods(gameTitleId, thirtyDayPopular DESC, popular DESC);
CREATE INDEX IF NOT EXISTS idx_mods_created  ON mods(gameTitleId, createDate DESC);
CREATE INDEX IF NOT EXISTS idx_mods_rating   ON mods(gameTitleId, currentRating DESC);

CREATE TABLE IF NOT EXISTS mod_images (
  imageId        TEXT PRIMARY KEY,
  modId          INTEGER NOT NULL REFERENCES mods(modId) ON DELETE CASCADE,
  displayIndex   INTEGER NOT NULL DEFAULT 0,
  imageName      TEXT NOT NULL,
  imageThumbnail TEXT,
  imageFileSize  REAL
);
CREATE INDEX IF NOT EXISTS idx_mod_images_mod ON mod_images(modId);

CREATE TABLE IF NOT EXISTS mod_tag_map (
  modId      INTEGER NOT NULL REFERENCES mods(modId) ON DELETE CASCADE,
  gameTitleId INTEGER NOT NULL,
  modTypeId  INTEGER NOT NULL,
  PRIMARY KEY (modId, modTypeId),
  FOREIGN KEY (gameTitleId, modTypeId) REFERENCES mod_types(gameTitleId, modTypeId)
);
CREATE INDEX IF NOT EXISTS idx_mod_tag_map_type ON mod_tag_map(gameTitleId, modTypeId);

CREATE TABLE IF NOT EXISTS mod_ratings (
  userId   INTEGER NOT NULL REFERENCES users(userId),
  modId    INTEGER NOT NULL REFERENCES mods(modId) ON DELETE CASCADE,
  rating   INTEGER NOT NULL CHECK (rating BETWEEN 1 AND 5),
  PRIMARY KEY (userId, modId)
);
CREATE INDEX IF NOT EXISTS idx_ratings_mod ON mod_ratings(modId);

CREATE TABLE IF NOT EXISTS reviews (
  reviewId         TEXT PRIMARY KEY,
  modId            INTEGER NOT NULL REFERENCES mods(modId) ON DELETE CASCADE,
  reviewerId       TEXT,
  reviewerName     TEXT,
  avatarUrl        TEXT,
  rating           INTEGER NOT NULL CHECK (rating BETWEEN 1 AND 5),
  title            TEXT NOT NULL DEFAULT '',
  reviewText       TEXT NOT NULL DEFAULT '',
  submitted        TEXT NOT NULL,
  itemVersion      TEXT NOT NULL DEFAULT '',
  locale           TEXT,
  helpfulPositive  INTEGER NOT NULL DEFAULT 0,
  helpfulNegative  INTEGER NOT NULL DEFAULT 0,
  helpfulnessVotes INTEGER NOT NULL DEFAULT 0,
  isInstalled      INTEGER NOT NULL DEFAULT 0,
  UNIQUE (modId, reviewerId)
);
CREATE INDEX IF NOT EXISTS idx_reviews_mod ON reviews(modId, submitted DESC);

CREATE TABLE IF NOT EXISTS mod_likes (
  userId  INTEGER NOT NULL REFERENCES users(userId),
  modId   INTEGER NOT NULL REFERENCES mods(modId) ON DELETE CASCADE,
  PRIMARY KEY (userId, modId)
);

CREATE TABLE IF NOT EXISTS subscriptions (
  userId        INTEGER NOT NULL REFERENCES users(userId),
  modId         INTEGER NOT NULL REFERENCES mods(modId) ON DELETE CASCADE,
  downloadedAt  TEXT,
  installedAt   TEXT,
  -- downloadCounted records that this user already contributed to
  -- mods.downloads, so Subscribe followed by Download counts once.
  downloadCounted INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (userId, modId)
);
CREATE INDEX IF NOT EXISTS idx_subscriptions_mod ON subscriptions(modId);

CREATE TABLE IF NOT EXISTS flags (
  flagId     INTEGER PRIMARY KEY AUTOINCREMENT,
  modId      INTEGER NOT NULL REFERENCES mods(modId) ON DELETE CASCADE,
  userId     INTEGER NOT NULL REFERENCES users(userId),
  reason     TEXT NOT NULL,
  boolValue  INTEGER,
  createdAt  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now'))
);
CREATE INDEX IF NOT EXISTS idx_flags_mod ON flags(modId);

CREATE TABLE IF NOT EXISTS polls (
  pollId          TEXT PRIMARY KEY,
  gameId          INTEGER NOT NULL REFERENCES games(gameTitleId),
  createdBy       TEXT,
  created         TEXT NOT NULL,
  startDateTime   TEXT NOT NULL,
  endDateTime     TEXT NOT NULL,
  expireDateTime  TEXT NOT NULL,
  metadata        TEXT NOT NULL DEFAULT '',
  icon            TEXT,
  isDefinition    INTEGER NOT NULL DEFAULT 0,
  totalVotes      INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS poll_questions (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  pollId      TEXT NOT NULL REFERENCES polls(pollId) ON DELETE CASCADE,
  sortOrder   INTEGER NOT NULL DEFAULT 0,
  content     TEXT NOT NULL,
  type        TEXT NOT NULL CHECK (type IN ('multiple-choice','text')),
  maxOptions  INTEGER NOT NULL DEFAULT 1,
  totalVotes  INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_poll_questions_poll ON poll_questions(pollId, sortOrder);

CREATE TABLE IF NOT EXISTS poll_options (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  questionId  INTEGER NOT NULL REFERENCES poll_questions(id) ON DELETE CASCADE,
  value       TEXT NOT NULL,
  voteCount   INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_poll_options_question ON poll_options(questionId);

CREATE TABLE IF NOT EXISTS poll_votes (
  userId      INTEGER NOT NULL REFERENCES users(userId),
  questionId  INTEGER NOT NULL REFERENCES poll_questions(id) ON DELETE CASCADE,
  optionId    INTEGER NOT NULL REFERENCES poll_options(id) ON DELETE CASCADE,
  PRIMARY KEY (userId, questionId)
);
`

// Migrate applies the schema and records the version.
func (s *SystemStore) Migrate() error {
	if _, err := s.db.Exec(schemaSQL); err != nil {
		return fmt.Errorf("apply schema: %w", err)
	}
	var current int
	err := s.db.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM _migrations`).Scan(&current)
	if err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	if current >= schemaVersion {
		return nil
	}
	_, err = s.db.Exec(`INSERT OR REPLACE INTO _migrations (version, appliedAt) VALUES (?, ?)`,
		schemaVersion, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("record schema version: %w", err)
	}
	return nil
}

// Ping reports whether the database answers.
func (s *SystemStore) Ping() error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return s.db.PingContext(ctx)
}

// SchemaVersion is the version recorded in _migrations.
func (s *SystemStore) SchemaVersion() (int, error) {
	var version int
	err := s.db.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM _migrations`).Scan(&version)
	return version, err
}
