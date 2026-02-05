package utils

import (
	"database/sql"

	model "github.com/ayden-boyko/Piranid/nodes/Notifications/models"
)

// NotifUpdater is a function that updates a notif into the database
func NotifUpdater(tx *sql.Tx, entry model.NotifEntry, isSent bool) error {

	stmt, err := tx.Prepare("UPDATE notifications SET sent=? WHERE id=? AND contact_info=?")
	if err != nil {
		return err
	}
	defer stmt.Close()

	_, err = stmt.Exec(isSent, entry.Id, entry.ContactInfo)
	if err != nil {
		return err
	}
	return err
}
