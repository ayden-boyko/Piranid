package utils

import (
	"database/sql"

	model "github.com/ayden-boyko/Piranid/nodes/Notifications/models"
)

// NotifDeleter removes a notification record.
//
// The previous statement embedded a literal tab character between DELETE and
// FROM, which is not valid SQL.
func NotifDeleter(tx *sql.Tx, entry model.NotifEntry) error {
	stmt, err := tx.Prepare(
		"DELETE FROM notifications WHERE service_id = ? AND contact_info = ?")
	if err != nil {
		return err
	}
	defer stmt.Close()

	res, err := stmt.Exec(entry.ServiceId, entry.ContactInfo)
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
