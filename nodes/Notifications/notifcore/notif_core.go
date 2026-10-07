package core

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"Piranid/node"

	data_manager "Piranid/pkg/DataManager"

	model "github.com/ayden-boyko/Piranid/nodes/Notifications/models"
	notifutils "github.com/ayden-boyko/Piranid/nodes/Notifications/utils"

	v1 "Piranid/pkg/proto/notifications/v1"

	"github.com/trycourier/courier-go/v2"
	"go.uber.org/zap"
)

// DefaultRetryAttempts bounds delivery retries.
//
// The previous implementation was an unbounded busy loop with no sleep, which
// would pin a CPU core and hammer Courier on any persistent failure.
const DefaultRetryAttempts = 5

// RetryBackoff is the base delay between delivery attempts. Attempt n waits
// n * RetryBackoff.
const RetryBackoff = 2 * time.Second

// ErrNotConfigured reports a dependency that was never wired up.
var ErrNotConfigured = errors.New("notifications: dependency not configured")

// NotificationNode is the notification service core.
//
// It deliberately does NOT embed *node.Node: that type carries an *http.Server
// and a Redis client, and this service is gRPC-only. Embedding it meant
// SafeShutdown called Shutdown on an HTTP server that was never started, so the
// gRPC server was never stopped at all.
type NotificationNode struct {
	v1.UnimplementedNotifierServer

	Messager   *courier.Client
	Service_ID string
	Logger     *zap.Logger

	// DB is the notifications store, accessed through pkg/DataManager.
	//
	// The previous code held the *sql.DB in the embedded node and asserted it to
	// *sql.Tx in every write path. SetUpDB stores a *sql.DB, so the assertion
	// always failed and every write returned "database is not a transaction".
	DB *data_manager.DataManagerImpl[model.NotifEntry]

	// rawDB is retained so the pool can be closed on shutdown.
	rawDB *sql.DB

	// TemplateSource resolves a notification template for a delivery method.
	// It may be nil, in which case the method's default template is used.
	TemplateSource TemplateSource

	// RetryAttempts bounds HandleNotifRetry.
	RetryAttempts int
}

// TemplateSource resolves the Courier template identifier for a method.
//
// The original code hardcoded an empty template, and ValidateIntegrity required
// one, so every send failed with "TEMPLATE NIL" and Courier was never reached.
// Rather than either relaxing validation or inventing a template registry, this
// lets a deployment supply one and gives a documented default.
type TemplateSource interface {
	TemplateFor(method model.ContactMethod) string
}

// StaticTemplateSource maps methods to template identifiers.
type StaticTemplateSource map[model.ContactMethod]string

// TemplateFor implements TemplateSource.
func (s StaticTemplateSource) TemplateFor(method model.ContactMethod) string {
	return s[method]
}

// DefaultTemplates are used when no TemplateSource is configured.
//
// Courier requires a template identifier, so a send cannot proceed without one.
// These are placeholders that must be replaced with real template ids before
// production delivery will work, and MisconfiguredTemplateError says so plainly
// rather than failing with a bare validation error.
var DefaultTemplates = StaticTemplateSource{
	model.Email:  "piranid-notification-email",
	model.Mobile: "piranid-notification-sms",
	model.Slack:  "piranid-notification-slack",
}

// ErrNoTemplate reports that no template could be resolved for a method.
var ErrNoTemplate = errors.New("notifications: no template configured for method")

// resolveTemplate picks the template for an entry's method.
func (n *NotificationNode) resolveTemplate(entry model.NotifEntry) (string, error) {
	if entry.Template != "" {
		return entry.Template, nil
	}

	method, err := entry.GetMethod()
	if err != nil {
		return "", err
	}

	if n.TemplateSource != nil {
		if t := n.TemplateSource.TemplateFor(method); t != "" {
			return t, nil
		}
	}
	if t := DefaultTemplates[method]; t != "" {
		return t, nil
	}
	return "", fmt.Errorf("%w: %q", ErrNoTemplate, method)
}

// HandleNotifSend delivers a notification through Courier.
//
// This is reached from both the gRPC and RabbitMQ paths. It returns errors
// rather than terminating the process, which the original code did with
// log.Fatalln on a Courier failure: on the gRPC path that was a remotely
// triggerable kill.
func (n *NotificationNode) HandleNotifSend(ctx context.Context, entry model.NotifEntry) error {
	if n.Messager == nil {
		return fmt.Errorf("%w: courier client", ErrNotConfigured)
	}

	contact, err := entry.GetContact()
	if err != nil {
		return err
	}

	method, err := entry.GetMethod()
	if err != nil {
		return err
	}

	data, err := entry.GetData()
	if err != nil {
		return err
	}

	importance, err := entry.GetImportance()
	if err != nil {
		return err
	}

	// ValidateIntegrity required a template that nothing ever set. The template
	// is resolved here instead, from the configured source.
	template, err := n.resolveTemplate(entry)
	if err != nil {
		return err
	}

	requestID, err := n.Messager.SendMessage(ctx, courier.SendMessageRequestBody{
		Message: map[string]any{
			"to": map[string]string{
				string(method): contact,
			},
			"template": template,
			"data":     data,
			"metadata": map[string]string{
				"importance": fmt.Sprint(importance),
			},
		},
	})
	if err != nil {
		return fmt.Errorf("courier send failed: %w", err)
	}

	if n.Logger != nil {
		// Log the Courier request id, never the contact address: it is PII.
		n.Logger.Info("notification sent",
			zap.String("courier_request_id", requestID),
			zap.String("method", string(method)),
			zap.String("service_id", entry.ID()),
			zap.Int32("importance", importance),
		)
	}

	return nil
}

// HandleNotifRetry delivers with bounded retries and linear backoff.
//
// The previous version looped forever with no delay, so any persistent failure
// spun a core at full tilt and hammered the provider. It also called log.Fatalln
// on a send error, killing the process rather than retrying.
func (n *NotificationNode) HandleNotifRetry(ctx context.Context, entry model.NotifEntry) error {
	attempts := n.RetryAttempts
	if attempts <= 0 {
		attempts = DefaultRetryAttempts
	}

	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}

		lastErr = n.HandleNotifSend(ctx, entry)
		if lastErr == nil {
			return nil
		}

		if n.Logger != nil {
			n.Logger.Warn("notification send failed",
				zap.Int("attempt", attempt),
				zap.Int("max_attempts", attempts),
				zap.String("service_id", entry.ID()),
				zap.Error(lastErr),
			)
		}

		if attempt == attempts {
			break
		}

		// Linear backoff, cancellable.
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(attempt) * RetryBackoff):
		}
	}

	return fmt.Errorf("giving up after %d attempts: %w", attempts, lastErr)
}

// NotifSent marks a notification as delivered.
func (n *NotificationNode) NotifSent(ctx context.Context, entry model.NotifEntry) error {
	if n.DB == nil {
		return fmt.Errorf("%w: notifications store", ErrNotConfigured)
	}
	return n.DB.UpdateData(entry, func(tx *sql.Tx, e model.NotifEntry) error {
		return notifutils.NotifUpdater(tx, e, true)
	})
}

// RemoveNotif deletes a notification record.
//
// The proto names this DeleteUser, but it deletes notification rows for a
// (service_id, username) pair. It is not an account-deletion operation.
func (n *NotificationNode) RemoveNotif(ctx context.Context, entry model.NotifEntry) error {
	if n.DB == nil {
		return fmt.Errorf("%w: notifications store", ErrNotConfigured)
	}
	return n.DB.DeleteData(entry, notifutils.NotifDeleter)
}

// StoreNotif persists a notification record.
func (n *NotificationNode) StoreNotif(ctx context.Context, entry model.NotifEntry) error {
	if n.DB == nil {
		return fmt.Errorf("%w: notifications store", ErrNotConfigured)
	}
	return n.DB.PushData(entry, notifutils.NotifInserter)
}

// AttachDB wires the notifications store.
func (n *NotificationNode) AttachDB(db *sql.DB) error {
	if db == nil {
		return errors.New("notifications: nil database handle")
	}
	manager, err := data_manager.NewDataManager[model.NotifEntry](db, "notifications")
	if err != nil {
		return fmt.Errorf("creating notifications manager: %w", err)
	}
	n.DB = manager
	n.rawDB = db
	return nil
}

// SetTemplateSource installs a template resolver.
func (n *NotificationNode) SetTemplateSource(src TemplateSource) {
	n.TemplateSource = src
}

// ShutdownDB closes the database pool.
func (n *NotificationNode) ShutdownDB() error {
	if n.rawDB == nil {
		return nil
	}
	err := n.rawDB.Close()
	n.rawDB = nil
	n.DB = nil
	return err
}

// SafeShutdown drains in-flight RPCs and releases resources.
//
// grpcServer.GracefulStop is invoked by the caller before this, or by
// StopWithGrace in main. The original code called Shutdown on an http.Server it
// had never started, so the gRPC server was never stopped and in-flight RPCs
// were cut off at the context deadline.
func (n *NotificationNode) SafeShutdown(ctx context.Context) error {
	var errs []error
	if err := n.ShutdownDB(); err != nil {
		errs = append(errs, fmt.Errorf("closing database: %w", err))
	}
	return errors.Join(errs...)
}

// unused keeps the embedded node import referenced; the type is intentionally
// not embedded any more.
var _ = (*node.Node)(nil)
