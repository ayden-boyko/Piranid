package utils

import (
	"database/sql"

	model "github.com/ayden-boyko/Piranid/nodes/Notifications/models"
)

// NotifInserter is a function that inserts a notif into the database
func NotifInserter(tx *sql.Tx, entry model.NotifEntry) error {

	stmt, err := tx.Prepare("INSERT INTO notifications (service_id, username, info, method, sent) VALUES (?, ?)")
	if err != nil {
		return err
	}
	defer stmt.Close()

	_, err = stmt.Exec(entry.Id, entry.ContactInfo, entry.Data, entry.Method, false) // false cause it hasn't been sent
	if err != nil {
		return err
	}
	return err
}
