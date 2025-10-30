package models

import "time"

type Entry struct {
	Id           uint64    `json:"id"`
	Base62Id     string    `json:"base62_id"`
	LongUrl      string    `json:"longurl"`
	Date_Created time.Time `json:"date_created"`
}
