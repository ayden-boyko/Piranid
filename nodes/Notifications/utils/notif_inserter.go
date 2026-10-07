package utils

import (
	"database/sql"

	model "github.com/ayden-boyko/Piranid/nodes/Notifications/models"
)

// NotifInserter stores a notification record.
//
// The previous statement listed 5 columns with 2 placeholders and bound 5
// arguments, so it could never have succeeded; it also bound entry.Data, a
// map[string]string, which database/sql cannot convert.
func NotifInserter(tx *sql.Tx, entry model.NotifEntry) error {
	data, err := entry.MarshalData()
	if err != nil {
		return err
	}

	stmt, err := tx.Prepare(`INSERT INTO notifications (
		service_id, contact_info, method, message_data,
		importance, template, sent, created_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	_, err = stmt.Exec(
		entry.ServiceId,
		entry.ContactInfo,
		string(entry.Method),
		data,
		entry.Importance,
		entry.Template,
		entry.Sent,
		entry.CreatedAt.UTC().Format(timeLayout),
	)
	return err
}
