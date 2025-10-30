package DataManager

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"Piranid/pkg/models"
)

type DataManagerImpl struct {
	db *sql.DB
}

func NewDataManager(db *sql.DB) (*DataManagerImpl, error) {
	if db == nil {
		return nil, errors.New("database connection is nil")
	}
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}
	return &DataManagerImpl{db: db}, nil
}

func (d *DataManagerImpl) GetEntry(id uint64) (string, error) {

	if err := d.db.Ping(); err != nil {
		return "database connection lost", fmt.Errorf("database connection lost: %w", err)
	}

	// begins a transaction
	tx, err := d.db.Begin()
	if err != nil {
		return "", fmt.Errorf("error starting transaction: %w", err)
	}

	// rollback the transaction if an error occurs
	defer tx.Rollback()

	rows, err := tx.Query("SELECT * FROM entries WHERE id = ?", id)
	if err != nil {
		return "Rows not found", err
	}

	defer rows.Close()

	if rows.Next() {
		var entry models.Entry
		if err := rows.Scan(&entry.Id, &entry.Base62Id, &entry.LongUrl, &entry.Date_Created); err != nil {
			return "error scanning", err
		}
		return entry.LongUrl, nil
	}
	// commit the transaction
	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("error committing transaction: %w", err)
	}
	return "No entry found", nil
}

func (d *DataManagerImpl) PushData(entry models.Entry) (string, error) {

	if err := d.db.Ping(); err != nil {
		return "", fmt.Errorf("database connection lost: %w", err)
	}

	// begins a transaction
	tx, err := d.db.Begin()
	if err != nil {
		return "", fmt.Errorf("error starting transaction: %w", err)
	}

	// rollback the transaction if an error occurs
	defer tx.Rollback()

	// checks if the entry already exists
	exists := ""
	rows := tx.QueryRow("SELECT Base62Id FROM entries WHERE LongUrl = ? LIMIT 1", entry.LongUrl)
	if err = rows.Scan(&exists); err != nil && err != sql.ErrNoRows {
		return "", fmt.Errorf("error querying database: %w", err)
	}

	// if the entry already exists
	if exists != "" {
		fmt.Println("Entry already exists: ", exists)
		// return the existing entry
		return exists, errors.New("entry already exists")
	}

	//before adding the long url, check if it has https or http
	if !strings.HasPrefix(entry.LongUrl, "http") {
		//add https if it doesn't
		entry.LongUrl = "https://" + entry.LongUrl
	}

	_, err = tx.Exec("INSERT INTO entries (id, base62Id, LongUrl, date_created) VALUES (?, ?, ?, ?)",
		entry.Id, entry.Base62Id, entry.LongUrl, entry.Date_Created)
	if err != nil {
		return "", fmt.Errorf("error executing database insert: %w", err)
	}

	err = tx.Commit()
	if err != nil {
		fmt.Println("error committing transaction: ", err)
		return "", fmt.Errorf("error committing transaction: %w", err)
	}

	// commit the transaction
	return "", err
}

func (d *DataManagerImpl) Close() {
	d.db.Close()
}

func (d *DataManagerImpl) Ping() error {
	return d.db.Ping()
}

func (d *DataManagerImpl) Stats() sql.DBStats {
	return d.db.Stats()
}
