package utils

import amqp "github.com/rabbitmq/amqp091-go"

func MapToTable(m map[string]string) amqp.Table {
	table := make(amqp.Table)
	for k, v := range m {
		table[k] = v
	}
	return table
}
