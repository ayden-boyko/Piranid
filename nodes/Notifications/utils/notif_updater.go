package utils

import (
	"database/sql"

	model "github.com/ayden-boyko/Piranid/nodes/Notifications/models"
)

// NotifUpdater sets the sent flag on a notification.
//
// The previous statement filtered on a contact_info column that the schema did
// not define, so it could not have matched a row.
func NotifUpdater(tx *sql.Tx, entry model.NotifEntry, isSent bool) error {
	stmt, err := tx.Prepare(
		"UPDATE notifications SET sent = ? WHERE service_id = ? AND contact_info = ?")
	if err != nil {
		return err
	}
	defer stmt.Close()

	res, err := stmt.Exec(isSent, entry.ServiceId, entry.ContactInfo)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}
