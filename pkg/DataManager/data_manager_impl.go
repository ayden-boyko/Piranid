package DataManager

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Define an interface that all entry types must implement
type Entry interface {
	GetID() (uint64, error)
	GetDateCreated() (*time.Time, error)
}

type DataManagerImpl[T Entry] struct {
	db        *sql.DB
	tableName string
}

// NewDataManager creates a new data manager with the given database connection and table name.
// It returns an error if the database connection is nil, or if the database connection is lost,
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
	return &DataManagerImpl[T]{
		db:        db,
		tableName: tableName,
	}, nil
}

func (d *DataManagerImpl[T]) GetEntry(key string, id string, scanner func(*sql.Rows) (T, error)) (T, error) {
	var zero T
	if err := d.db.Ping(); err != nil {
		return zero, fmt.Errorf("database connection lost: %w", err)
	}

	rows, err := d.db.Query("SELECT * FROM %s WHERE %s = %d", d.tableName, key, id)
	if err != nil {
		return zero, err
	}
	defer rows.Close()

	if rows.Next() {
		return scanner(rows)
	}

	return zero, sql.ErrNoRows
}

func (d *DataManagerImpl[T]) PushData(entry T, inserter func(*sql.Tx, T) error) error {
	if err := d.db.Ping(); err != nil {
		return fmt.Errorf("database connection lost: %w", err)
	}

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
