package dbutil

import (
	"database/sql"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// TestPragmasAreActuallyApplied guards a silent failure mode.
//
// The driver reads repeated "_pragma=name(value)" parameters. An earlier
// implementation emitted "_pragma_name=value", which the driver ignores without
// complaint, so busy_timeout was never set and concurrent writes failed with
// SQLITE_BUSY. This test reads the pragma back from a live connection.
func TestPragmasAreActuallyApplied(t *testing.T) {
	db := mustOpen(t)

	var journalMode string
	if err := db.QueryRow("PRAGMA journal_mode").Scan(&journalMode); err != nil {
		t.Fatalf("reading journal_mode: %v", err)
	}
	if !strings.EqualFold(journalMode, "wal") {
		t.Errorf("journal_mode = %q, want wal", journalMode)
	}

	// The whole point of the pragma: SQLite should wait for a write lock
	// instead of failing immediately.
	var busyTimeout int
	if err := db.QueryRow("PRAGMA busy_timeout").Scan(&busyTimeout); err != nil {
		t.Fatalf("reading busy_timeout: %v", err)
	}
	if busyTimeout != 5000 {
		t.Errorf("busy_timeout = %d, want 5000", busyTimeout)
	}
}

// TestBeginIsImmediate covers the deferred-transaction upgrade problem.
//
// With the default deferred BEGIN, a transaction takes a read lock first and
// must upgrade it to a write lock. SQLite deliberately does not invoke the busy
// handler for that upgrade, so the request fails with SQLITE_BUSY regardless of
// busy_timeout. BEGIN IMMEDIATE acquires the write lock up front, where the busy
// handler does apply.
func TestBeginIsImmediate(t *testing.T) {
	dsn, err := sqliteDSN(filepath.Join(t.TempDir(), "x.db"))
	if err != nil {
		t.Fatalf("sqliteDSN: %v", err)
	}
	if !strings.Contains(dsn, "_txlock=immediate") {
		t.Fatalf("dsn = %q, want _txlock=immediate", dsn)
	}
}

// TestConcurrentWritersDoNotFail is the end-to-end guarantee: several writers
// contending for SQLite's single write lock must all eventually succeed, rather
// than some failing with "database is locked".
func TestConcurrentWritersDoNotFail(t *testing.T) {
	db := mustOpen(t)

	const workers = 8
	var wg sync.WaitGroup
	errs := make([]error, workers)
	start := make(chan struct{})

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			<-start
			_, err := db.Exec("INSERT INTO t (v) VALUES (?)", n)
			errs[n] = err
		}(i)
	}

	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("worker %d failed: %v", i, err)
		}
	}

	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM t").Scan(&count); err != nil {
		t.Fatalf("counting: %v", err)
	}
	if count != workers {
		t.Errorf("wrote %d rows, want %d", count, workers)
	}
}

func TestApplySchema(t *testing.T) {
	db := mustOpen(t)
	if err := ApplySchema(db, "CREATE TABLE a (x INTEGER); CREATE TABLE b (y TEXT);"); err != nil {
		t.Fatalf("ApplySchema: %v", err)
	}

	for _, table := range []string{"a", "b"} {
		var name string
		err := db.QueryRow(
			"SELECT name FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&name)
		if err != nil {
			t.Errorf("table %q not created: %v", table, err)
		}
	}
}

func mustOpen(t *testing.T) *sql.DB {
	t.Helper()
	db, err := OpenSQLite(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	if _, err := db.Exec("CREATE TABLE IF NOT EXISTS t (v INTEGER)"); err != nil {
		t.Fatalf("creating table: %v", err)
	}
	return db
}
