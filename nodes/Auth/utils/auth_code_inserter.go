package utils

import (
	"database/sql"

	model "github.com/ayden-boyko/Piranid/nodes/Auth/models"
)

// AuthCodeInserter stores a freshly issued authorization code.
//
// The column names here must match Schema.sql exactly. The previous version
// wrote to (auth_code, expires) while the table defined (authcode, expires), so
// every insert failed on a missing column.
func AuthCodeInserter(tx *sql.Tx, entry model.AuthCodeEntry) error {
	stmt, err := tx.Prepare(`INSERT INTO auth_codes (
		code, client_id, subject, scope, redirect_uri,
		code_challenge, code_challenge_method, state, nonce,
		issued_at, expires_at, consumed_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	_, err = stmt.Exec(
		entry.Code,
		entry.ClientId,
		entry.Subject,
		entry.Scope,
		entry.RedirectURI,
		entry.CodeChallenge,
		entry.CodeChallengeMethod,
		entry.State,
		entry.Nonce,
		entry.IssuedAt,
		entry.ExpiresAt,
		entry.ConsumedAt,
	)
	return err
}
