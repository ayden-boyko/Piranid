package utils

import (
	"database/sql"

	model "github.com/ayden-boyko/Piranid/nodes/Notifications/models"
)

// NotifScanner reads one notification row.
//
// The scan order must match model.NotifColumns exactly.
func NotifScanner(rows *sql.Rows) (model.NotifEntry, error) {
	var (
		e       model.NotifEntry
		method  string
		data    string
		sent    int
		created string
	)

	if err := rows.Scan(
		&e.ServiceId,
		&e.ContactInfo,
		&method,
		&data,
		&e.Importance,
		&e.Template,
		&sent,
		&created,
	); err != nil {
		return model.NotifEntry{}, err
	}

	e.Method = model.ContactMethod(method)
	e.Sent = sent != 0

	if err := e.UnmarshalData(data); err != nil {
		return model.NotifEntry{}, err
	}
	if t, err := timeParse(created); err == nil {
		e.CreatedAt = t
	}

	return e, nil
}
