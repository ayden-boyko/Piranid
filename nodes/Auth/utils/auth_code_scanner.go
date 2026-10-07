package utils

import (
	"database/sql"

	model "github.com/ayden-boyko/Piranid/nodes/Auth/models"
)

// AuthCodeScanner reads one authorization code row.
//
// The scan order must match model.AuthCodeColumns exactly — that list is what
// gets passed to GetEntry/ConsumeEntry as the SELECT column list. The previous
// version scanned only (authcode, expires) from a SELECT *, which fails because
// the row has more columns than the scanner consumes.
func AuthCodeScanner(rows *sql.Rows) (model.AuthCodeEntry, error) {
	var e model.AuthCodeEntry

	err := rows.Scan(
		&e.Code,
		&e.ClientId,
		&e.Subject,
		&e.Scope,
		&e.RedirectURI,
		&e.CodeChallenge,
		&e.CodeChallengeMethod,
		&e.State,
		&e.Nonce,
		&e.IssuedAt,
		&e.ExpiresAt,
		&e.ConsumedAt,
	)
	if err != nil {
		return model.AuthCodeEntry{}, err
	}
	return e, nil
}
