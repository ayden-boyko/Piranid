package DataManager

import (
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"time"
)

// Define an interface that all entry types must implement
type Entry interface {
	GetID() (string, error)
	GetDateCreated() (*time.Time, error)
}

// ErrNoMatch is returned by GetEntry/ConsumeEntry when a query yields no rows.
// Callers should test for it with errors.Is rather than comparing a string.
var ErrNoMatch = errors.New("no matching row")

// identifierRe matches bare SQL identifiers. Table and column names cannot be
// bound as query parameters, so they are interpolated into the SQL text and
// must be validated first. This is what keeps the interpolation safe.
var identifierRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// validateIdentifiers rejects anything that is not a bare SQL identifier.
// Without this, a caller-supplied column name would be an injection vector.
func validateIdentifiers(names ...string) error {
	for _, n := range names {
		if !identifierRe.MatchString(n) {
			return fmt.Errorf("invalid SQL identifier %q", n)
		}
	}
	return nil
}

type DataManagerImpl[T Entry] struct {
	db        *sql.DB
	tableName string
}

// NewDataManager creates a new data manager with the given database connection and table name.
// It returns an error if the database connection is nil, if the database connection is lost,
// or if the table name is empty.
func NewDataManager[T Entry](db *sql.DB, tableName string) (*DataManagerImpl[T], error) {
	if db == nil {
		return nil, errors.New("database connection is nil")
	}
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}
	if tableName == "" {
		return nil, errors.New("table name cannot be empty")
	}
	if err := validateIdentifiers(tableName); err != nil {
		return nil, fmt.Errorf("invalid table name: %w", err)
	}
	return &DataManagerImpl[T]{
		db:        db,
		tableName: tableName,
	}, nil
}

// GetEntry selects the first row matching columns=id and hands it to scanner.
//
// columns must list exactly the columns the scanner reads, in order. Callers
// used to rely on SELECT * with a scanner reading fewer columns than the table
// has, which fails with a "expected N destination arguments" scan error; making
// the column list explicit keeps the SQL and the scanner in agreement.
//
// The value is bound as a parameter. It used to be interpolated with %d, which
// both mangled string keys and made the statement uncacheable.
func (d *DataManagerImpl[T]) GetEntry(columns []string, key string, id string, scanner func(*sql.Rows) (T, error)) (T, error) {
	var zero T
	if len(columns) == 0 {
		return zero, errors.New("no columns specified")
	}
	if err := validateIdentifiers(append(columns, key)...); err != nil {
		return zero, err
	}

	// #nosec G202 -- identifiers validated by validateIdentifiers above;
	// only the value is bound, which is the parameterizable part.
	query := fmt.Sprintf("SELECT %s FROM %s WHERE %s = ?",
		joinIdentifiers(columns), d.tableName, key)

	rows, err := d.db.Query(query, id)
	if err != nil {
		return zero, err
	}
	defer rows.Close()

	if rows.Next() {
		entry, err := scanner(rows)
		if err != nil {
			return zero, err
		}
		return entry, nil
	}
	if err := rows.Err(); err != nil {
		return zero, err
	}

	return zero, ErrNoMatch
}

// ConsumeEntry atomically reads a row and deletes it inside one transaction.
//
// Use this for single-use resources such as OAuth authorization codes: a plain
// GetEntry followed by DeleteData leaves a window in which two concurrent
// requests can both redeem the same code. Here the SELECT and the DELETE share
// a transaction, and the row is marked consumed before the transaction commits.
//
// It returns ErrNoMatch when the row does not exist or was already consumed.
func (d *DataManagerImpl[T]) ConsumeEntry(columns []string, key string, id string, scanner func(*sql.Rows) (T, error), deleter func(*sql.Tx, T) error) (T, error) {
	var zero T
	if len(columns) == 0 {
		return zero, errors.New("no columns specified")
	}
	if err := validateIdentifiers(append(columns, key)...); err != nil {
		return zero, err
	}

	tx, err := d.db.Begin()
	if err != nil {
		return zero, fmt.Errorf("error starting transaction: %w", err)
	}
	defer tx.Rollback()

	// #nosec G202 -- identifiers validated above; value bound as parameter.
	query := fmt.Sprintf("SELECT %s FROM %s WHERE %s = ?",
		joinIdentifiers(columns), d.tableName, key)

	rows, err := tx.Query(query, id)
	if err != nil {
		return zero, err
	}

	var entry T
	found := false
	if rows.Next() {
		entry, err = scanner(rows)
		if err != nil {
			rows.Close()
			return zero, err
		}
		found = true
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return zero, err
	}
	rows.Close()

	if !found {
		return zero, ErrNoMatch
	}

	if err := deleter(tx, entry); err != nil {
		return zero, err
	}
	if err := tx.Commit(); err != nil {
		return zero, fmt.Errorf("error committing transaction: %w", err)
	}

	return entry, nil
}

func (d *DataManagerImpl[T]) PushData(entry T, inserter func(*sql.Tx, T) error) error {
	tx, err := d.db.Begin()
	if err != nil {
		return fmt.Errorf("error starting transaction: %w", err)
	}
	defer tx.Rollback()

	if err := inserter(tx, entry); err != nil {
		return err
	}

	return tx.Commit()
}

func (d *DataManagerImpl[T]) UpdateData(entry T, updater func(*sql.Tx, T) error) error {
	tx, err := d.db.Begin()
	if err != nil {
		return fmt.Errorf("error starting transaction: %w", err)
	}
	defer tx.Rollback()

	if err := updater(tx, entry); err != nil {
		return err
	}

	return tx.Commit()
}

func (d *DataManagerImpl[T]) DeleteData(entry T, deleter func(*sql.Tx, T) error) error {
	tx, err := d.db.Begin()
	if err != nil {
		return fmt.Errorf("error starting transaction: %w", err)
	}
	defer tx.Rollback()

	if err := deleter(tx, entry); err != nil {
		return err
	}

	return tx.Commit()
}

// joinIdentifiers renders validated identifiers as a comma-separated list.
func joinIdentifiers(names []string) string {
	out := ""
	for i, n := range names {
		if i > 0 {
			out += ", "
		}
		out += n
	}
	return out
}
