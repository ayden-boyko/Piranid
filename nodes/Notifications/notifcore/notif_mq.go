package core

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strconv"
	"sync"

	model "github.com/ayden-boyko/Piranid/nodes/Notifications/models"
	amqp "github.com/rabbitmq/amqp091-go"
)

// from the message queue, these messages trigger notifications to be sent

func (n *NotificationNode) StartMQListener(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// when a message is received, call the HandleNotifSend function with the appropriate parameters
	MQ_PORT := os.Getenv("RABBIT_MQ_PORT")
	if MQ_PORT == "" {
		log.Panic("RABBIT_MQ_PORT environment variable not set")
	}

	WORKER_POOL_SIZE, err := strconv.Atoi(os.Getenv("EVENT_SERVICE_WORKER_POOL_SIZE"))
	if err != nil || WORKER_POOL_SIZE == 0 {
		log.Panic("EVENT_SERVICE_WORKER_POOL_SIZE environment variable not set or invalid")
	}

	fmt.Println("Dialing Message Queue...")
	conn, err := amqp.Dial(fmt.Sprintf("amqp://guest:guest@rabbitmq:%s/", MQ_PORT))
	if err != nil {
		log.Panicf("%s: %s", "Failed to connect to RabbitMQ", err)
	}
	defer conn.Close()

	channel, err := conn.Channel()
	if err != nil {
		log.Panicf("%s: %s", "Failed to open a channel", err)
	}
	defer channel.Close()

	queue, err := channel.QueueDeclare(
		os.Getenv("RABBIT_MQ_QUEUE_NAME"), // name
		false,                             // durable
		false,                             // delete when unused
		false,                             // exclusive
		false,                             // no-wait
		nil,                               // arguments
	)
	if err != nil {
		log.Panicf("%s: %s", "Failed to declare a queue", err)
	}

	fmt.Println("Started MQ Listener...")
	var wg sync.WaitGroup

	select {
	case <-ctx.Done():
		wg.Wait()
		fmt.Println("Shutting down MQ Listener...")
		return
	default:
		msgs, err := channel.Consume(
			queue.Name, // queue
			"",         // consumer
			true,       // auto-ack
			false,      // exclusive
			false,      // no-local
			false,      // no-wait
			nil,        // args
		)
		if err != nil {
			log.Panicf("%s: %s", "Failed to register a consumer", err)
		}
		for i := 0; i < WORKER_POOL_SIZE; i++ {
			wg.Add(1)
			go n.notificationWorker(msgs, &wg, ctx)
		}
	}

	// No need to check for sigterm or sigint here
	// deferring the closing of the connection and channel will ensure that they are closed when the function exits,
	//  which will happen when the main function receives a sigterm or sigint and calls SafeShutdown,
	//  which will call this function's context's Done channel, which will cause this function to exit
}

func (n *NotificationNode) notificationWorker(msgs <-chan amqp.Delivery, wg *sync.WaitGroup, ctx context.Context) {

	defer wg.Done()

	for msg := range msgs {

		// TODO: check that the unmarshaling is working correctly & that the message body is in the correct format
		entry := model.NotifEntry{}
		err := json.Unmarshal(msg.Body, &entry)
		if err != nil {
			log.Printf("Failed to unmarshal message body: %s", err)
			continue
		}

		// log the entry that was received
		log.Printf("Received message: %+v", entry)

		// do something with the message
		err = n.HandleNotifSend(ctx, entry)
		if err != nil {
			log.Printf("Failed to handle notification: %s", err)
		}

		// acknowledge the message
		msg.Ack(false)
	}
}
