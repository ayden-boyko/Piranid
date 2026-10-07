package utils

import (
	"database/sql"

	model "github.com/ayden-boyko/Piranid/nodes/Auth/models"
)

// CredentialsColumns is the exact column list CredentialsScanner reads, in
// order. Pass it to GetEntry/ConsumeEntry as the SELECT column list.
var CredentialsColumns = []string{
	"username",
	"client_id",
	"client_name",
	"redirect_uri",
	"user_email",
	"hashed_password",
	"client_secret",
	"service_id",
}

// CredentialsScanner reads one client/credential row.
//
// The scan order must match CredentialsColumns exactly.
func CredentialsScanner(rows *sql.Rows) (model.AuthEntry, error) {
	var e model.AuthEntry

	err := rows.Scan(
		&e.Username,
		&e.ClientId,
		&e.ClientName,
		&e.RedirectURI,
		&e.UserEmail,
		&e.HashedPassword,
		&e.ClientSecret,
		&e.ServiceId,
	)
	if err != nil {
		return model.AuthEntry{}, err
	}
	return e, nil
}
