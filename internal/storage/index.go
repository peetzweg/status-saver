package storage

import (
	"database/sql"
	"fmt"

	_ "github.com/mattn/go-sqlite3"
)

type Index struct {
	db *sql.DB
}

func OpenIndex(path string) (*Index, error) {
	db, err := sql.Open("sqlite3", "file:"+path+"?_journal=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("open index db: %w", err)
	}
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS seen_messages (
			msg_id      TEXT NOT NULL,
			sender_jid  TEXT NOT NULL,
			received_at INTEGER NOT NULL,
			path        TEXT NOT NULL,
			PRIMARY KEY (msg_id, sender_jid)
		);
		CREATE INDEX IF NOT EXISTS idx_seen_received_at ON seen_messages(received_at);
	`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("init schema: %w", err)
	}
	return &Index{db: db}, nil
}

func (i *Index) Close() error { return i.db.Close() }

// MarkSeen inserts (msgID, senderJID) with metadata. Returns inserted=false
// when the row already existed — caller uses this to skip duplicate work.
func (i *Index) MarkSeen(msgID, senderJID string, receivedAtUnix int64, storedPath string) (bool, error) {
	res, err := i.db.Exec(
		`INSERT OR IGNORE INTO seen_messages (msg_id, sender_jid, received_at, path) VALUES (?, ?, ?, ?)`,
		msgID, senderJID, receivedAtUnix, storedPath,
	)
	if err != nil {
		return false, fmt.Errorf("mark seen: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func (i *Index) HasSeen(msgID, senderJID string) (bool, error) {
	var one int
	err := i.db.QueryRow(
		`SELECT 1 FROM seen_messages WHERE msg_id=? AND sender_jid=? LIMIT 1`,
		msgID, senderJID,
	).Scan(&one)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("has seen: %w", err)
	}
	return true, nil
}

// PruneOlderThan deletes rows with received_at < cutoffUnix. Used by the
// rotation job after it removes the on-disk files.
func (i *Index) PruneOlderThan(cutoffUnix int64) (int64, error) {
	res, err := i.db.Exec(`DELETE FROM seen_messages WHERE received_at < ?`, cutoffUnix)
	if err != nil {
		return 0, fmt.Errorf("prune index: %w", err)
	}
	return res.RowsAffected()
}
