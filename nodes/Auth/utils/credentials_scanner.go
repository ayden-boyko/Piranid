package utils

import (
	"database/sql"

	model "github.com/ayden-boyko/Piranid/nodes/Auth/models"
)

func CredentialsScanner(rows *sql.Rows) (model.AuthEntry, error) {
	var u model.AuthEntry

	err := rows.Scan(&u.Username, &u.HashedPassword, &u.UserEmail, &u.ClientSecret, &u.ClientId, &u.ServiceId)

	return u, err
}
