// Package dbutil opens databases with settings that a concurrent service
// actually needs.
package dbutil

import (
	"database/sql"
	"fmt"
	"net/url"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// SQLitePragmas are applied to every Piranid SQLite connection.
//
// busy_timeout is the important one. Without it, SQLite returns SQLITE_BUSY the
// instant another connection holds a write lock, and Go's database/sql pool
// surfaces that as "database is locked". Since SQLite allows exactly one writer,
// an authorization server handling simultaneous token requests fails requests
// rather than queueing them. A busy timeout makes writers wait for the lock.
//
// journal_mode=WAL lets readers proceed while a writer holds the lock, instead
// of blocking every read behind every write.
var SQLitePragmas = map[string]string{
	"journal_mode": "WAL",
	"busy_timeout": "5000",
	"foreign_keys": "ON",
	"synchronous":  "NORMAL",
}

// TxLockImmediate makes every transaction begin with BEGIN IMMEDIATE instead of
// the default deferred BEGIN.
//
// This is required for correctness under concurrency, and busy_timeout alone is
// not enough. A deferred transaction starts by taking only a read lock; when it
// later writes, SQLite has to upgrade that read lock to a write lock. SQLite
// deliberately does NOT invoke the busy handler for that upgrade, because the
// transaction might already hold a read lock that another writer is waiting on,
// so waiting could deadlock. The result is an immediate SQLITE_BUSY that no
// busy_timeout setting will absorb.
//
// BEGIN IMMEDIATE acquires the write lock up front, where the busy handler does
// apply, so the request waits and then succeeds. The auth node needs this: two
// simultaneous token requests must not cause one to fail.
const TxLockImmediate = "immediate"

// OpenSQLite opens a SQLite database at path with the pragmas above applied.
//
// path may be a bare file path or a file: URL. Any existing query parameters
// are preserved, so a caller can override a pragma if it must.
func OpenSQLite(path string) (*sql.DB, error) {
	dsn, err := sqliteDSN(path)
	if err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("opening sqlite database: %w", err)
	}

	// SQLite serializes writes anyway. Capping the pool avoids a pile of
	// connections contending for the single write lock.
	db.SetMaxOpenConns(8)
	db.SetMaxIdleConns(4)
	db.SetConnMaxLifetime(time.Hour)

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("connecting to sqlite database: %w", err)
	}

	return db, nil
}

// sqliteDSN renders a DSN carrying the pragma query parameters.
func sqliteDSN(path string) (string, error) {
	// A bare path becomes a file: URL so the query parameters can be attached.
	if !strings.HasPrefix(path, "file:") {
		path = "file:" + path
	}

	u, err := url.Parse(path)
	if err != nil {
		return "", fmt.Errorf("parsing database path %q: %w", path, err)
	}

	q := u.Query()

	// The driver reads repeated "_pragma" parameters of the form
	// "name(value)", not a "_pragma_name" key. Getting this wrong silently
	// applies nothing at all, which leaves busy_timeout unset.
	existing := map[string]bool{}
	for _, spec := range q["_pragma"] {
		name, _, _ := strings.Cut(spec, "(")
		existing[strings.ToLower(strings.TrimSpace(name))] = true
	}
	for k, v := range SQLitePragmas {
		// Do not override a pragma the caller set explicitly.
		if existing[strings.ToLower(k)] {
			continue
		}
		q.Add("_pragma", k+"("+v+")")
	}

	if q.Get("_txlock") == "" {
		q.Set("_txlock", TxLockImmediate)
	}
	u.RawQuery = q.Encode()

	return u.String(), nil
}

// ApplySchema executes a SQL script against db.
//
// Multi-statement Exec is supported by the SQLite driver used here, which is
// what lets Schema.sql hold the whole table definition.
func ApplySchema(db *sql.DB, script string) error {
	if _, err := db.Exec(script); err != nil {
		return fmt.Errorf("applying schema: %w", err)
	}
	return nil
}
