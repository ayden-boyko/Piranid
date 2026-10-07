package utils

import "time"

// timeLayout is the storage format for created_at, kept in one place so writes
// and reads agree.
const timeLayout = time.RFC3339Nano

func timeParse(raw string) (time.Time, error) { return time.Parse(timeLayout, raw) }
