package utils

import (
	"database/sql"

	model "github.com/ayden-boyko/Piranid/nodes/Auth/models"
)

func AuthCodeScanner(rows *sql.Rows) (model.AuthCodeEntry, error) {
	var u model.AuthCodeEntry

	err := rows.Scan(&u.AuthCode, &u.Expires)

	return u, err
}
