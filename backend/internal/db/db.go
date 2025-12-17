package db

import (
	"database/sql"
	"log"

	_ "github.com/mattn/go-sqlite3"
)

type DB struct {
	*sql.DB
}

func Open(path string) (*DB, error) {
	database, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, err
	}

	if _, err := database.Exec(`PRAGMA foreign_keys = ON;`); err != nil {
		return nil, err
	}

	if err := migrate(database); err != nil {
		return nil, err
	}

	return &DB{database}, nil
}

func migrate(db *sql.DB) error {
	log.Println("running migrations")

	schema := `
CREATE TABLE IF NOT EXISTS users (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    discord_id       TEXT NOT NULL UNIQUE,
    discord_username TEXT,
    created_at       DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at       DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS subscriptions (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id     INTEGER NOT NULL,
    line_id     TEXT NOT NULL,
    via_dm      INTEGER NOT NULL DEFAULT 1,   -- 0/1 for false/true
    via_guild   INTEGER NOT NULL DEFAULT 0,
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(user_id, line_id),
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS line_status (
    line_id      TEXT PRIMARY KEY,
    status       TEXT NOT NULL,
    header       TEXT,
    body         TEXT,
    effect       TEXT,
    alert_id     TEXT,
    content_hash TEXT,
    updated_at   DATETIME NOT NULL
);

CREATE TABLE IF NOT EXISTS alerts (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    alert_id     TEXT NOT NULL,
    line_id      TEXT NOT NULL,
    old_status   TEXT,
    new_status   TEXT,
    header       TEXT,
    body         TEXT,
    effect       TEXT,
    started_at   DATETIME,
    created_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_alerts_line_created_at 
ON alerts (line_id, created_at);

CREATE TABLE IF NOT EXISTS notifications (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id       INTEGER NOT NULL,
    alert_id      INTEGER NOT NULL,
    line_id       TEXT NOT NULL,
    channel_type  TEXT NOT NULL,   -- 'dm' for now
    status        TEXT NOT NULL,   -- 'pending', 'sent', 'failed'
    last_error    TEXT,
    created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    sent_at       DATETIME,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    FOREIGN KEY (alert_id) REFERENCES alerts(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_notifications_status 
ON notifications(status);
`

	_, err := db.Exec(schema)
	return err
}
