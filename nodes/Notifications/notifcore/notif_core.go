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
	"github.com/ayden-boyko/Piranid/nodes/Notifications/utils"

	v1 "Piranid/pkg/proto/notifications/v1"

	"github.com/trycourier/courier-go/v2"
)

type NotificationNode struct {
	*node.Node
	v1.UnimplementedNotifierServer
	Messager   *courier.Client
	Service_ID string
}

// TODO Caching

// * Only to be used internally, hence the name
// sends a notif, will be used by gRPC and MQ
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

	data, err := entry.GetData()
	if err != nil {
		return err
	}

	fmt.Println("Sending notification...")

	requestID, err := n.Messager.SendMessage(
		ctx,
		courier.SendMessageRequestBody{
			Message: map[string]interface{}{
				"to": map[string]string{
					string(method): contact,
				},
				"template": template,
				"data":     data,
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

// * Only to be used internally, hence the name
// retries sending a notif
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

			data, err := entry.GetData()
			if err != nil {
				return err
			}

			requestID, err := n.Messager.SendMessage(
				context.Background(),
				courier.SendMessageRequestBody{
					Message: map[string]interface{}{
						"to": map[string]string{
							string(method): contact,
						},
						"template": template,
						"data":     data,
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

// switches notif status to sent
func (n *NotificationNode) NotifSent(ctx context.Context, entry model.NotifEntry) error {
	dbTx, ok := n.DB.(*sql.Tx)
	if !ok {
		return errors.New("database is not a transaction")
	}
	err := utils.NotifUpdater(dbTx, entry, true)
	return err
}

func (n *NotificationNode) RemoveNotif(ctx context.Context, entry model.NotifEntry) error {
	dbTx, ok := n.DB.(*sql.Tx)
	if !ok {
		return errors.New("database is not a transaction")
	}
	err := utils.NotifDeleter(dbTx, entry)
	return err
}

// adds notif to DB after sending, dont save notif data, LOOK AT NOTEBOOK for more info
func (n *NotificationNode) StoreNotif(ctx context.Context, entry model.NotifEntry) error {
	dbTx, ok := n.DB.(*sql.Tx)
	if !ok {
		return errors.New("database is not a transaction")
	}
	err := utils.NotifInserter(dbTx, entry)
	return err
}

// ! NOT NEEDED, NO HTTP happening, set up messager in node startup in main
func (n *NotificationNode) RegisterRoutes() {
	// TODO Actual route registration for logging server
	// TODO set up adding and removing from db
	// TODO, add notif to db before sending
	// TODO add notif to cache before sending, once sent remove from cache
	n.Node.Router.HandleFunc("/notification_test", func(w http.ResponseWriter, r *http.Request) {
		fmt.Println("Sending notification...")
		fmt.Fprint(w, "Sending notification...")
		client := courier.CreateClient(
			os.Getenv("COURIER_TOKEN"), nil,
		)
		n.Messager = client
		requestID, err := n.Messager.SendMessage(
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
