package DataManager

import (
	"database/sql"
	"errors"
	"fmt"
)

// Define an interface that all entry types must implement
type Entry interface {
	GetID() uint64
	// Add other common methods if needed
}

type DataManagerImpl[T Entry] struct {
	db *sql.DB
}

func NewDataManager[T Entry](db *sql.DB) (*DataManagerImpl[T], error) {
	if db == nil {
		return nil, errors.New("database connection is nil")
	}
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}
	return &DataManagerImpl[T]{db: db}, nil
}

func (d *DataManagerImpl[T]) GetEntry(id uint64, scanner func(*sql.Rows) (T, error)) (T, error) {
	var zero T
	if err := d.db.Ping(); err != nil {
		return zero, fmt.Errorf("database connection lost: %w", err)
	}

	rows, err := d.db.Query("SELECT * FROM entries WHERE id = ?", id)
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
