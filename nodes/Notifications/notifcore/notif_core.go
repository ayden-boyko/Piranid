package core

import (
	"Piranid/node"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"

	model "github.com/ayden-boyko/Piranid/nodes/Notifications/models"

	"github.com/trycourier/courier-go/v2"
)

type NotificationNode struct {
	*node.Node
	Notifier   *courier.Client
	Service_ID string // TODO SHOULD services ID be public or private?
}

// TODO Caching

func (n *NotificationNode) GetServiceID() string { return n.Service_ID }

// when sending a notif, its implied that importance is >= 5
func (n *NotificationNode) HandleNotifSend(ctx context.Context, entry model.NotifEntry) error {
	err := entry.ValidateIntegrity()
	if err != nil {
		return err
	}

	method, err := entry.GetMethod()
	if err != nil {
		return err
	}

	contact, err := entry.GetContact()
	if err != nil {
		return err
	}

	template, err := entry.GetTemplate()
	if err != nil {
		return err
	}

	info, err := entry.GetInfo()
	if err != nil {
		return err
	}

	fmt.Println("Sending notification...")

	requestID, err := n.Notifier.SendMessage(
		context.Background(),
		courier.SendMessageRequestBody{
			Message: map[string]interface{}{
				"to": map[string]string{
					string(method): contact,
				},
				"template": template,
				"data": map[string]string{
					"data": info,
				},
			},
		},
	)

	if err != nil {
		log.Fatalln(err)
	}
	log.Println(requestID)
	fmt.Println("Notification sent...")

	// todo add to log that notif was sent

	return nil
}

func (n *NotificationNode) HandleNotifRetry(ctx context.Context, entry model.NotifEntry) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:

			err := entry.ValidateIntegrity()
			if err != nil {
				return err
			}

			method, err := entry.GetMethod()
			if err != nil {
				return err
			}

			contact, err := entry.GetContact()
			if err != nil {
				return err
			}

			template, err := entry.GetTemplate()
			if err != nil {
				return err
			}

			info, err := entry.GetInfo()
			if err != nil {
				return err
			}

			requestID, err := n.Notifier.SendMessage(
				context.Background(),
				courier.SendMessageRequestBody{
					Message: map[string]interface{}{
						"to": map[string]string{
							string(method): contact,
						},
						"template": template,
						"data": map[string]string{
							"data": info,
						},
					},
				},
			)

			if err != nil {
				log.Fatalln(err)
			}
			log.Println(requestID)
			fmt.Println("Notification sent...")
		}
	}

}

func (n *NotificationNode) RegisterRoutes() {
	// TODO Actual route registration for logging server
	n.Node.Router.HandleFunc("/notification_test", func(w http.ResponseWriter, r *http.Request) {
		fmt.Println("Sending notification...")
		fmt.Fprint(w, "Sending notification...")
		client := courier.CreateClient(
			os.Getenv("COURIER_TOKEN"), nil,
		)
		n.Notifier = client
		requestID, err := n.Notifier.SendMessage(
			context.Background(),
			courier.SendMessageRequestBody{
				Message: map[string]interface{}{
					"to": map[string]string{
						"email": "aydenboyko@gmail.com",
					},
					"template": "2GPARRPY3S4WHDKV8G07V5JKNNHZ",
					"data": map[string]string{
						"data": "HAHAHA THIS IS TESTING DTATA",
					},
				},
			},
		)

		if err != nil {
			log.Fatalln(err)
		}
		log.Println(requestID)
		fmt.Println("Notification sent...")
	})

}

func (l *NotificationNode) ShutdownDB() error {
	db := l.Node.GetDB()
	if sqliteDB, ok := db.(*sql.DB); ok {
		sqliteDB.Close()
		fmt.Println("Database closed...")
		return nil
	}
	return errors.New("database is not *sql.DB, is type " + fmt.Sprintf("%T", db))
}

// SafeShutdown is a function that gracefully stops the server and closes the database connection.
func (n *NotificationNode) SafeShutdown(ctx context.Context) error {
	// Shutdown the server
	if err := n.Server.Shutdown(ctx); err != nil {
		return err
	}

	// Close the database connection
	if err := n.ShutdownDB(); err != nil {
		return err
	}
	return nil
}
