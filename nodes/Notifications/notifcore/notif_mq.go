package core

import (
	"fmt"
	"log"
	"os"

	amqp "github.com/rabbitmq/amqp091-go"
)

// from the message queue, these messages trigger notifications to be sent

func (n *NotificationNode) StartMQListener() {
	// TODO connect to the message queue and listen for messages
	// when a message is received, call the HandleNotifSend function with the appropriate parameters
	MQ_PORT := os.Getenv("RABBIT_MQ_PORT")
	if MQ_PORT == "" {
		log.Panic("RABBIT_MQ_PORT environment variable not set")
	}

	fmt.Println("Dialing Message Queue...")
	conn, err := amqp.Dial(fmt.Sprintf("amqp://guest:guest@rabbitmq:%s/", MQ_PORT))
	if err != nil {
		log.Panicf("%s: %s", "Failed to connect to RabbitMQ", err)
	}
	defer conn.Close()

	// TODO receive messages from the message queue
}

func (n *NotificationNode) connectToRoute(route string) (*amqp.Connection, error) {
	// TODO connect to the message queue and return the connection
	return nil, nil
}
