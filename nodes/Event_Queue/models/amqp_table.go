package models

import (
	"strconv"
	"strings"

	amqp "github.com/rabbitmq/amqp091-go"
)

// typedTable converts a string map into an AMQP field table with correctly
// typed values.
//
// AMQP field tables are typed. The previous implementation kept every value a Go
// string, so a numeric argument such as x-message-ttl or x-max-length arrived
// at the broker as a string and was rejected with a channel-level
// PRECONDITION_FAILED. A nil map now yields a nil table rather than a non-nil
// empty one, so the broker sees "no arguments" as intended.
func typedTable(m map[string]string) amqp.Table {
	if len(m) == 0 {
		return nil
	}

	table := make(amqp.Table, len(m))
	for k, v := range m {
		table[k] = typedValue(v)
	}
	return table
}

// typedValue infers the narrowest AMQP type that represents a string faithfully.
//
// An integer argument must not become a float, or the broker rejects it. A
// string that merely looks numeric (a token, an id) still converts exactly, so
// inference is safe for values that arrived as digits.
func typedValue(v string) any {
	switch {
	case isBoolLiteral(v):
		b, err := strconv.ParseBool(v)
		if err == nil {
			return b
		}
	case looksInteger(v):
		if i, err := strconv.ParseInt(v, 10, 64); err == nil {
			return i
		}
	}
	return v
}

func isBoolLiteral(v string) bool {
	switch strings.ToLower(v) {
	case "true", "false":
		return true
	}
	return false
}

// looksInteger reports whether v is a plain base-10 integer.
//
// The explicit sign and digit checks reject floats, exponents and hex, all of
// which would lose fidelity if coerced to int64.
func looksInteger(v string) bool {
	if v == "" {
		return false
	}
	body := v
	if body[0] == '-' || body[0] == '+' {
		body = body[1:]
	}
	if body == "" {
		return false
	}
	for i := 0; i < len(body); i++ {
		if body[i] < '0' || body[i] > '9' {
			return false
		}
	}
	return true
}
