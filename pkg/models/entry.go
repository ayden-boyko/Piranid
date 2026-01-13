package models

import (
	"errors"
	"time"
)

type Entry struct {
	Id           uint64    `json:"id"`
	Date_Created time.Time `json:"date_created"`
}

func (e Entry) GetID() (uint64, error) {
	if e.Id == 0 {
		return 0, errors.New("ID NIL")
	}
	return e.Id, nil
}

func (e Entry) GetDateCreated() (time.Time, error) {
	if e.Date_Created.IsZero() {
		return time.Time{}, errors.New("DATE_CREATED NIL")
	}
	return e.Date_Created, nil
}
