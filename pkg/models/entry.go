package models

import "time"

type Entry struct {
	Id           uint64    `json:"id"`
	Date_Created time.Time `json:"date_created"`
}

func (e Entry) GetID() uint64 {
	return e.Id
}

func (e Entry) GetDateCreated() time.Time {
	return e.Date_Created
}
