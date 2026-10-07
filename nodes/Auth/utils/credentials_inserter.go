package utils

import (
	"database/sql"

	model "github.com/ayden-boyko/Piranid/nodes/Auth/models"
)

// CredentialsInserter registers a client and its owning user.
//
// redirect_uri and client_name are now included. The previous statement
// omitted redirect_uri even though every redirect_uri equality check in the
// handlers read it from this row.
func CredentialsInserter(tx *sql.Tx, entry model.AuthEntry) error {
	stmt, err := tx.Prepare(`INSERT INTO credentials (
		date_created, client_id, client_secret, client_name, redirect_uri,
		username, user_email, hashed_password, service_id
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	_, err = stmt.Exec(
		entry.Date_Created.Format(timeLayout),
		entry.ClientId,
		entry.ClientSecret,
		entry.ClientName,
		entry.RedirectURI,
		entry.Username,
		entry.UserEmail,
		entry.HashedPassword,
		entry.ServiceId,
	)
	return err
}
