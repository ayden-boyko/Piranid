package utils

import (
	"database/sql"

	model "github.com/ayden-boyko/Piranid/nodes/Auth/models"
)

func AuthCodeDeleter(tx *sql.Tx, entry model.AuthCodeEntry) error {

	stmt, err := tx.Prepare("DELETE ROW FROM auth_codes WHERE auth_code = ?")
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
