package utils

import (
	"database/sql"

	model "github.com/ayden-boyko/Piranid/nodes/Auth/models"
)

// AuthCodeDeleter removes a code once it has been redeemed.
//
// This is passed to ConsumeEntry, which runs it in the same transaction as the
// SELECT, so the read-and-delete pair is atomic and a code cannot be redeemed
// twice.
//
// The previous version used "DELETE ROW FROM", which is not valid SQL in any
// dialect, and passed two arguments to a statement with one placeholder.
func AuthCodeDeleter(tx *sql.Tx, entry model.AuthCodeEntry) error {
	stmt, err := tx.Prepare("DELETE FROM auth_codes WHERE code = ?")
	if err != nil {
		return err
	}
	defer stmt.Close()

	res, err := stmt.Exec(entry.Code)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// AuthCodeSweeper deletes codes that expired before the given Unix timestamp.
// Used to keep the table from growing without bound; a production deployment
// would schedule this on an interval.
func AuthCodeSweeper(tx *sql.Tx, before int64) error {
	stmt, err := tx.Prepare("DELETE FROM auth_codes WHERE expires_at < ?")
	if err != nil {
		return err
	}
	defer stmt.Close()

	_, err = stmt.Exec(before)
	return err
}
