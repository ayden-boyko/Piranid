package utils

import (
	"database/sql"

	model "github.com/ayden-boyko/Piranid/nodes/Auth/models"
)

func CredentialsInserter(tx *sql.Tx, entry model.AuthEntry) error {

	stmt, err := tx.Prepare("INSERT INTO credentials (username, hashed_password, user_email, client_secret, client_id, service_id) VALUES (?, ?, ?, ?, ?, ?)")
	if err != nil {
		return err
	}
	defer stmt.Close()

	_, err = stmt.Exec(entry.Username, entry.HashedPassword, entry.UserEmail, entry.ClientSecret, entry.ClientId, entry.ServiceId)
	if err != nil {
		return err
	}
	return err
}
