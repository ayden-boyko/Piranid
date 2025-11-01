package utils

import (
	"database/sql"

	model "github.com/ayden-boyko/Piranid/nodes/Auth/models"
)

func AuthCodeInserter(tx *sql.Tx, entry model.AuthCodeEntry) error {

	stmt, err := tx.Prepare("INSERT INTO auth_codes (auth_code, expires) VALUES (?, ?)")
	if err != nil {
		return err
	}
	defer stmt.Close()

	_, err = stmt.Exec(entry.AuthCode, entry.Expires)
	if err != nil {
		return err
	}
	return err
}
