package utils

import (
	"database/sql"

	model "github.com/ayden-boyko/Piranid/nodes/Notifications/models"
)

// NotifDeleter is a function that deletes a notif into the database
// Deleted Notifs usually have a low priority level, important ones may be kept for later use
func NotifDeleter(tx *sql.Tx, entry model.NotifEntry) error {

	stmt, err := tx.Prepare("DELETE	FROM notifications WHERE id=? AND contact_info=?")
	if err != nil {
		return err
	}
	defer stmt.Close()

	_, err = stmt.Exec(entry.Id, entry.ContactInfo)
	if err != nil {
		return err
	}
	return err
}
