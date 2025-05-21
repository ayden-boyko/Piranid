package internal

import (
	"github.com/google/uuid"
)

// Shared helper functions
func NewServiceID(prefix string) string {
	return prefix + "-" + uuid.New().String()
}
