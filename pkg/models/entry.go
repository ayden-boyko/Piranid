package models

import (
	"errors"
	"time"
)

type Entry struct {
	Id           string    `json:"id"`
	Date_Created time.Time `json:"date_created"`
}

func (e Entry) GetID() (string, error) {
	if e.Id == "" {
		return "", errors.New("ID NIL")
	}
	return e.Id, nil
}

func (e Entry) GetDateCreated() (time.Time, error) {
	if e.Date_Created.IsZero() {
		return time.Time{}, errors.New("DATE_CREATED NIL")
	}
	return e.Date_Created, nil
}
