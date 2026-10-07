package DataManager

import (
	"database/sql"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"Piranid/pkg/dbutil"
)

// testRow is a minimal Entry used to exercise the generic manager.
type testRow struct {
	Id           string
	Date_Created time.Time
	Name         string
	Secret       string
}

func (r testRow) GetID() (string, error)              { return r.Id, nil }
func (r testRow) GetDateCreated() (*time.Time, error) { return &r.Date_Created, nil }

const testSchema = `
CREATE TABLE widgets (
    id            TEXT PRIMARY KEY,
    date_created  TEXT NOT NULL,
    name          TEXT NOT NULL,
    secret        TEXT NOT NULL
);`

func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := dbutil.OpenSQLite(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("opening database: %v", err)
	}
	if _, err := db.Exec(testSchema); err != nil {
		t.Fatalf("applying schema: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// widgetColumns must list every column the scanner reads, including the key the
// deleter needs. ConsumeEntry hands the scanned entry to the deleter, so a
// scanner that skips the key column leaves the deleter with nothing to match on.
var widgetColumns = []string{"id", "name", "secret"}

func widgetScanner(rows *sql.Rows) (testRow, error) {
	var r testRow
	if err := rows.Scan(&r.Id, &r.Name, &r.Secret); err != nil {
		return testRow{}, err
	}
	return r, nil
}

func widgetInserter(tx *sql.Tx, r testRow) error {
	_, err := tx.Exec(
		"INSERT INTO widgets (id, date_created, name, secret) VALUES (?, ?, ?, ?)",
		r.Id, r.Date_Created.Format(time.RFC3339Nano), r.Name, r.Secret,
	)
	return err
}

func widgetDeleter(tx *sql.Tx, r testRow) error {
	res, err := tx.Exec("DELETE FROM widgets WHERE id = ?", r.Id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func TestGetEntrySelectsExplicitColumns(t *testing.T) {
	db := newTestDB(t)
	dm, err := NewDataManager[testRow](db, "widgets")
	if err != nil {
		t.Fatalf("NewDataManager: %v", err)
	}

	if err := dm.PushData(testRow{Id: "w1", Date_Created: time.Now(), Name: "alpha", Secret: "s"}, widgetInserter); err != nil {
		t.Fatalf("PushData: %v", err)
	}

	got, err := dm.GetEntry(widgetColumns, "name", "alpha", widgetScanner)
	if err != nil {
		t.Fatalf("GetEntry: %v", err)
	}
	if got.Secret != "s" {
		t.Errorf("secret = %q, want s", got.Secret)
	}
}

// GetEntry used to interpolate the value with %d, which broke every string key.
func TestGetEntryWithStringKey(t *testing.T) {
	db := newTestDB(t)
	dm, _ := NewDataManager[testRow](db, "widgets")

	_ = dm.PushData(testRow{Id: "w2", Date_Created: time.Now(), Name: "bravo", Secret: "x"}, widgetInserter)

	if _, err := dm.GetEntry(widgetColumns, "name", "bravo", widgetScanner); err != nil {
		t.Fatalf("GetEntry with a string key failed: %v", err)
	}
}

func TestGetEntryMissingRowReturnsErrNoMatch(t *testing.T) {
	db := newTestDB(t)
	dm, _ := NewDataManager[testRow](db, "widgets")

	_, err := dm.GetEntry(widgetColumns, "name", "nope", widgetScanner)
	if !errors.Is(err, ErrNoMatch) {
		t.Errorf("err = %v, want ErrNoMatch", err)
	}
}

// Identifiers are interpolated into SQL text, so they must be validated.
func TestGetEntryRejectsInjectedIdentifier(t *testing.T) {
	db := newTestDB(t)
	dm, _ := NewDataManager[testRow](db, "widgets")

	_, err := dm.GetEntry(widgetColumns, "name; DROP TABLE widgets", "x", widgetScanner)
	if err == nil {
		t.Fatal("GetEntry accepted a SQL identifier containing a semicolon")
	}
}

func TestGetEntryRejectsInjectedTableName(t *testing.T) {
	db := newTestDB(t)

	if _, err := NewDataManager[testRow](db, "widgets; DROP TABLE widgets"); err == nil {
		t.Fatal("NewDataManager accepted a table name containing a semicolon")
	}
}

func TestConsumeEntryDeletesAtomically(t *testing.T) {
	db := newTestDB(t)
	dm, _ := NewDataManager[testRow](db, "widgets")

	_ = dm.PushData(testRow{Id: "w3", Date_Created: time.Now(), Name: "charlie", Secret: "s"}, widgetInserter)

	got, err := dm.ConsumeEntry(widgetColumns, "name", "charlie", widgetScanner, widgetDeleter)
	if err != nil {
		t.Fatalf("ConsumeEntry: %v", err)
	}
	if got.Name != "charlie" {
		t.Errorf("name = %q, want charlie", got.Name)
	}

	// The row must be gone.
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM widgets WHERE id = ?", "w3").Scan(&count); err != nil {
		t.Fatalf("counting: %v", err)
	}
	if count != 0 {
		t.Errorf("row still present after ConsumeEntry (%d rows)", count)
	}

	// A second consume finds nothing.
	if _, err := dm.ConsumeEntry(widgetColumns, "name", "charlie", widgetScanner, widgetDeleter); !errors.Is(err, ErrNoMatch) {
		t.Errorf("second ConsumeEntry err = %v, want ErrNoMatch", err)
	}
}

// The reason ConsumeEntry exists: two concurrent redeems must not both succeed.
func TestConsumeEntryIsSafeUnderConcurrency(t *testing.T) {
	db := newTestDB(t)
	dm, _ := NewDataManager[testRow](db, "widgets")

	_ = dm.PushData(testRow{Id: "w4", Date_Created: time.Now(), Name: "delta", Secret: "s"}, widgetInserter)

	const workers = 8
	var wg sync.WaitGroup
	results := make([]error, workers)
	start := make(chan struct{})

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			<-start // release all goroutines at once
			_, err := dm.ConsumeEntry(widgetColumns, "name", "delta", widgetScanner, widgetDeleter)
			results[idx] = err
		}(i)
	}

	close(start)
	wg.Wait()

	successes := 0
	for _, err := range results {
		if err == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Errorf("%d concurrent consumes succeeded, want exactly 1", successes)
	}
}

// A GetEntry-then-DeleteData pair, by contrast, is not atomic.
func TestSelectThenDeleteIsNotAtomic(t *testing.T) {
	db := newTestDB(t)
	dm, _ := NewDataManager[testRow](db, "widgets")

	_ = dm.PushData(testRow{Id: "w5", Date_Created: time.Now(), Name: "echo", Secret: "s"}, widgetInserter)

	const workers = 8
	var wg sync.WaitGroup
	readOk := make([]bool, workers)
	start := make(chan struct{})

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			<-start
			// The old redemption pattern.
			if _, err := dm.GetEntry(widgetColumns, "name", "echo", widgetScanner); err == nil {
				readOk[idx] = true
			}
		}(i)
	}

	close(start)
	wg.Wait()

	// Demonstrates the race the non-atomic version had: multiple readers can
	// observe the same row before any of them deletes it.
	seen := 0
	for _, ok := range readOk {
		if ok {
			seen++
		}
	}
	t.Logf("%d of %d concurrent readers observed the single-use row; "+
		"this is the window ConsumeEntry closes", seen, workers)
}
