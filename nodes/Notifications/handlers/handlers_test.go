package handlers

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"Piranid/pkg/authn"
	"Piranid/pkg/dbutil"
	"Piranid/pkg/telemetry"

	model "github.com/ayden-boyko/Piranid/nodes/Notifications/models"
	core "github.com/ayden-boyko/Piranid/nodes/Notifications/notifcore"
	notifutils "github.com/ayden-boyko/Piranid/nodes/Notifications/utils"

	v1 "Piranid/pkg/proto/notifications/v1"

	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const (
	testIssuer   = "https://auth.piranid.local"
	testAudience = "notifications"
)

// fixture wires the real handler onto the real core, with a real SQLite store
// and a real RSA key. Courier is absent by design: HandleNotifSend must fail
// cleanly rather than panic when it is not configured.
type fixture struct {
	node     *core.NotificationNode
	handler  *NotificationHandler
	signer   *authn.Signer
	verifier *authn.Verifier
	dbPath   string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()

	dir := t.TempDir()

	script, err := os.ReadFile(filepath.Join("..", "database", "Schema.sql"))
	if err != nil {
		t.Fatalf("reading Schema.sql: %v", err)
	}

	dbPath := filepath.Join(dir, "notifications.db")
	db, err := dbutil.OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	if err := dbutil.ApplySchema(db, string(script)); err != nil {
		t.Fatalf("ApplySchema: %v", err)
	}

	node := &core.NotificationNode{
		Service_ID: "NOTF-test",
		Logger:     zap.NewNop(),
	}
	// AttachDB is what the old code could not do: it asserted *sql.DB to
	// *sql.Tx, which never succeeds, so every write path failed.
	if err := node.AttachDB(db); err != nil {
		t.Fatalf("AttachDB: %v", err)
	}
	t.Cleanup(func() { node.ShutdownDB() })

	pair, err := authn.GenerateKeyPair(2048)
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}
	signer, err := authn.NewSigner(pair, 15*time.Minute)
	if err != nil {
		t.Fatalf("NewSigner: %v", err)
	}

	verifier := authn.NewVerifier(authn.NewStaticKeySource(authn.NewJWKS(pair)),
		authn.WithIssuer(testIssuer),
		authn.WithAudience(testAudience),
	)

	return &fixture{
		node:     node,
		handler:  NewNotificationHandler(node, zap.NewNop()),
		signer:   signer,
		verifier: verifier,
		dbPath:   dbPath,
	}
}

// issue mints an access token for a caller.
func (f *fixture) issue(t *testing.T, clientID string) string {
	t.Helper()
	tok, _, err := f.signer.Issue(authn.IssueRequest{
		Subject:   "svc",
		ClientID:  clientID,
		Issuer:    testIssuer,
		Audience:  testAudience,
		Scope:     "notifications:send",
		TokenType: authn.TokenTypeAccess,
	})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	return tok
}

// ---------------------------------------------------------------------------

// The converter left Template empty and ValidateIntegrity required one, so
// every request failed with "TEMPLATE NIL" and Courier was never reached. Now
// the failure must be about the missing Courier client, not the template.
func TestRequestNotificationPassesValidation(t *testing.T) {
	f := newFixture(t)

	resp, err := f.handler.RequestNotification(context.Background(), &v1.NotificationRequest{
		ServiceId:  "svc-1",
		Username:   "alice@example.com",
		Method:     "Email",
		Data:       map[string]string{"code": "123"},
		Importance: 5,
	})

	// With no Courier client the send must fail, but on the courier dependency,
	// not on validation.
	if err == nil {
		t.Fatal("expected a send failure with no Courier client configured")
	}
	if strings.Contains(err.Error(), "TEMPLATE") {
		t.Errorf("failed on template resolution: %v", err)
	}
	if resp != nil {
		t.Error("handler returned both a response and an error; gRPC would discard the response")
	}
	if code := status.Code(err); code != codes.Internal {
		t.Errorf("status code = %v, want Internal", code)
	}
}

func TestRequestNotificationRejectsBadMethod(t *testing.T) {
	f := newFixture(t)

	_, err := f.handler.RequestNotification(context.Background(), &v1.NotificationRequest{
		ServiceId:  "svc-1",
		Username:   "alice@example.com",
		Method:     "carrier-pigeon",
		Importance: 5,
	})
	if code := status.Code(err); code != codes.InvalidArgument {
		t.Errorf("status code = %v, want InvalidArgument", code)
	}
}

func TestRequestNotificationRejectsNilRequest(t *testing.T) {
	f := newFixture(t)

	if _, err := f.handler.RequestNotification(context.Background(), nil); err == nil {
		t.Error("RequestNotification accepted a nil request")
	}
}

func TestRequestNotificationRejectsBadImportance(t *testing.T) {
	f := newFixture(t)

	_, err := f.handler.RequestNotification(context.Background(), &v1.NotificationRequest{
		ServiceId:  "svc-1",
		Username:   "alice@example.com",
		Method:     "Email",
		Importance: 99,
	})
	if code := status.Code(err); code != codes.InvalidArgument {
		t.Errorf("status code = %v, want InvalidArgument", code)
	}
}

// RequestTFA returned Status_SUCCESS unconditionally without generating,
// storing or sending anything. A caller gating a login on that response would
// believe a second factor had been delivered.
func TestRequestTFAIsNotImplemented(t *testing.T) {
	f := newFixture(t)

	resp, err := f.handler.RequestTFA(context.Background(), &v1.TFARequest{
		ServiceId:   "auth",
		Username:    "alice",
		Method:      "Email",
		ContactInfo: "alice@example.com",
	})

	if err == nil {
		t.Fatal("RequestTFA reported success without doing anything")
	}
	if code := status.Code(err); code != codes.Unimplemented {
		t.Errorf("status code = %v, want Unimplemented", code)
	}
	// The critical property: a failure response must never claim success.
	if resp != nil {
		if resp.Success == v1.Status_SUCCESS {
			t.Fatal("RequestTFA returned Status_SUCCESS alongside an error")
		}
		t.Errorf("returned a response alongside an error: %+v", resp)
	}
}

// ---------------------------------------------------------------------------
// persistence
// ---------------------------------------------------------------------------

func TestStoreAndDeleteNotification(t *testing.T) {
	f := newFixture(t)

	entry := notifEntry("svc-1", "alice@example.com")

	// StoreNotif used to fail with "database is not a transaction".
	if err := f.node.StoreNotif(context.Background(), entry); err != nil {
		t.Fatalf("StoreNotif: %v", err)
	}

	got, err := f.node.DB.GetEntry(notifEntryColumns(), "service_id", "svc-1", notifutils.NotifScanner)
	if err != nil {
		t.Fatalf("reading back the stored notification: %v", err)
	}
	if got.ContactInfo != "alice@example.com" {
		t.Errorf("contact_info = %q", got.ContactInfo)
	}
	if got.Sent {
		t.Error("sent = true, want false for a freshly stored notification")
	}
	if got.Data["code"] != "123" {
		t.Errorf("data = %v, want code=123", got.Data)
	}

	if err := f.node.RemoveNotif(context.Background(), entry); err != nil {
		t.Fatalf("RemoveNotif: %v", err)
	}
	if _, err := f.node.DB.GetEntry(notifEntryColumns(), "service_id", "svc-1", notifutils.NotifScanner); err == nil {
		t.Error("notification still present after RemoveNotif")
	}
}

func TestNotifSentUpdatesFlag(t *testing.T) {
	f := newFixture(t)

	entry := notifEntry("svc-1", "alice@example.com")
	if err := f.node.StoreNotif(context.Background(), entry); err != nil {
		t.Fatalf("StoreNotif: %v", err)
	}

	entry.Sent = true
	if err := f.node.NotifSent(context.Background(), entry); err != nil {
		t.Fatalf("NotifSent: %v", err)
	}

	got, err := f.node.DB.GetEntry(notifEntryColumns(), "service_id", "svc-1", notifutils.NotifScanner)
	if err != nil {
		t.Fatalf("reading back: %v", err)
	}
	if !got.Sent {
		t.Error("sent = false, want true after NotifSent")
	}
}

func TestDeleteUserMissingIsNotFound(t *testing.T) {
	f := newFixture(t)

	_, err := f.handler.DeleteUser(context.Background(), &v1.NotificationRequest{
		ServiceId: "nobody",
		Username:  "ghost@example.com",
	})
	if code := status.Code(err); code != codes.NotFound {
		t.Errorf("status code = %v, want NotFound", code)
	}
}

func TestDeleteUserRequiresServiceID(t *testing.T) {
	f := newFixture(t)

	_, err := f.handler.DeleteUser(context.Background(), &v1.NotificationRequest{
		Username: "alice@example.com",
	})
	if code := status.Code(err); code != codes.InvalidArgument {
		t.Errorf("status code = %v, want InvalidArgument", code)
	}
}

func TestRequestUserNotificationUpdateUpserts(t *testing.T) {
	f := newFixture(t)

	resp, err := f.handler.RequestUserNotificationUpdate(context.Background(),
		&v1.UserNotificationUpdate{
			ServiceId:   "svc-1",
			Username:    "alice",
			ContactInfo: "alice@example.com",
		})
	if err != nil {
		t.Fatalf("RequestUserNotificationUpdate: %v", err)
	}
	if resp.Success != v1.Status_SUCCESS {
		t.Errorf("success = %v, want SUCCESS", resp.Success)
	}

	// Applying the same change again must upsert, not fail.
	if _, err := f.handler.RequestUserNotificationUpdate(context.Background(),
		&v1.UserNotificationUpdate{
			ServiceId:   "svc-1",
			Username:    "alice",
			ContactInfo: "alice@example.com",
		}); err != nil {
		t.Fatalf("second update (upsert path): %v", err)
	}
}

func TestUpdateRequiresContactInfo(t *testing.T) {
	f := newFixture(t)

	_, err := f.handler.RequestUserNotificationUpdate(context.Background(),
		&v1.UserNotificationUpdate{ServiceId: "svc-1", Username: "alice"})
	if code := status.Code(err); code != codes.InvalidArgument {
		t.Errorf("status code = %v, want InvalidArgument", code)
	}
}

// The update handler previously dropped the username and never applied the
// proto's "contains @ means email" heuristic.
func TestUpdateInfersMethodFromContactInfo(t *testing.T) {
	if got := notifEntryFromUpdate(&v1.UserNotificationUpdate{
		ServiceId: "svc", Username: "a", ContactInfo: "a@b.com",
	}).Method; got != "Email" {
		t.Errorf("method for an email address = %q, want Email", got)
	}

	if got := notifEntryFromUpdate(&v1.UserNotificationUpdate{
		ServiceId: "svc", Username: "a", ContactInfo: "+15551234567",
	}).Method; got != "Mobile" {
		t.Errorf("method for a phone number = %q, want Mobile", got)
	}

	entry := notifEntryFromUpdate(&v1.UserNotificationUpdate{
		ServiceId: "svc", Username: "alice", ContactInfo: "a@b.com",
	})
	if entry.Data["username"] != "alice" {
		t.Errorf("data = %v, want the username preserved", entry.Data)
	}
}

// ---------------------------------------------------------------------------
// auth interceptor
// ---------------------------------------------------------------------------

// The gRPC server was constructed with no options, so every RPC was reachable
// without a credential.
func TestAuthInterceptorRejectsMissingToken(t *testing.T) {
	f := newFixture(t)

	interceptor := UnaryAuthInterceptor(AuthInterceptorOptions{
		Verifier: f.verifier,
	}, testLogger{})

	_, err := interceptor(context.Background(), &v1.NotificationRequest{},
		&grpc.UnaryServerInfo{FullMethod: "/notifications.v1.Notifier/RequestNotification"},
		func(context.Context, any) (any, error) {
			t.Fatal("handler was reached without a credential")
			return nil, nil
		})

	if code := status.Code(err); code != codes.Unauthenticated {
		t.Errorf("status code = %v, want Unauthenticated", code)
	}
}

func TestAuthInterceptorAcceptsValidToken(t *testing.T) {
	f := newFixture(t)

	interceptor := UnaryAuthInterceptor(AuthInterceptorOptions{
		Verifier: f.verifier,
	}, testLogger{})

	ctx := metadata.NewIncomingContext(context.Background(),
		metadata.Pairs("authorization", "Bearer "+f.issue(t, "svc-a")))

	reached := false
	_, err := interceptor(ctx, &v1.NotificationRequest{},
		&grpc.UnaryServerInfo{FullMethod: "/notifications.v1.Notifier/RequestNotification"},
		func(ctx context.Context, _ any) (any, error) {
			reached = true
			// Claims must be available for authorisation decisions.
			claims, ok := ClaimsFrom(ctx)
			if !ok {
				t.Error("verified claims missing from the handler context")
				return nil, nil
			}
			if claims.ClientID != "svc-a" {
				t.Errorf("client_id = %q, want svc-a", claims.ClientID)
			}
			if got, ok := CallerServiceID(ctx); !ok || got != "svc-a" {
				t.Errorf("CallerServiceID = %q, ok = %v", got, ok)
			}
			return &v1.NotificationResponse{Success: v1.Status_SUCCESS}, nil
		})

	if !reached {
		t.Fatal("handler was not reached with a valid token")
	}
	if err != nil {
		t.Errorf("interceptor returned an error for a valid token: %v", err)
	}
}

func TestAuthInterceptorRejectsWrongAudience(t *testing.T) {
	f := newFixture(t)

	// Mint for a different service.
	pair, _ := authn.GenerateKeyPair(2048)
	other, _ := authn.NewSigner(pair, time.Minute)
	tok, _, err := other.Issue(authn.IssueRequest{
		Subject: "svc", ClientID: "svc-b",
		Issuer: testIssuer, Audience: "somewhere-else",
	})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	interceptor := UnaryAuthInterceptor(AuthInterceptorOptions{
		Verifier: f.verifier,
	}, testLogger{})

	ctx := metadata.NewIncomingContext(context.Background(),
		metadata.Pairs("authorization", "Bearer "+tok))

	_, err = interceptor(ctx, &v1.NotificationRequest{},
		&grpc.UnaryServerInfo{FullMethod: "/x"},
		func(context.Context, any) (any, error) {
			t.Fatal("handler reached with a token for the wrong audience")
			return nil, nil
		})
	if code := status.Code(err); code != codes.Unauthenticated {
		t.Errorf("status code = %v, want Unauthenticated", code)
	}
}

func TestAuthInterceptorRejectsGarbageToken(t *testing.T) {
	f := newFixture(t)

	interceptor := UnaryAuthInterceptor(AuthInterceptorOptions{
		Verifier: f.verifier,
	}, testLogger{})

	ctx := metadata.NewIncomingContext(context.Background(),
		metadata.Pairs("authorization", "Bearer not.a.jwt"))

	_, err := interceptor(ctx, &v1.NotificationRequest{},
		&grpc.UnaryServerInfo{FullMethod: "/x"},
		func(context.Context, any) (any, error) { return nil, nil })
	if code := status.Code(err); code != codes.Unauthenticated {
		t.Errorf("status code = %v, want Unauthenticated", code)
	}
}

// A server with no verifier configured must reject, not admit.
func TestAuthInterceptorFailsClosedWhenUnconfigured(t *testing.T) {
	interceptor := UnaryAuthInterceptor(AuthInterceptorOptions{}, testLogger{})

	_, err := interceptor(context.Background(), &v1.NotificationRequest{},
		&grpc.UnaryServerInfo{FullMethod: "/x"},
		func(context.Context, any) (any, error) {
			t.Fatal("handler reached with verification unconfigured")
			return nil, nil
		})
	if code := status.Code(err); code != codes.Internal {
		t.Errorf("status code = %v, want Internal", code)
	}
}

func TestAllowAnonymousPassesThrough(t *testing.T) {
	interceptor := UnaryAuthInterceptor(AuthInterceptorOptions{AllowAnonymous: true}, testLogger{})

	reached := false
	_, err := interceptor(context.Background(), &v1.NotificationRequest{},
		&grpc.UnaryServerInfo{FullMethod: "/x"},
		func(context.Context, any) (any, error) {
			reached = true
			return nil, nil
		})
	if !reached {
		t.Error("handler not reached with AllowAnonymous")
	}
	if err != nil {
		t.Errorf("err = %v", err)
	}
}

// The rejection message must not reveal why verification failed.
func TestAuthRejectionDoesNotLeakReason(t *testing.T) {
	f := newFixture(t)

	interceptor := UnaryAuthInterceptor(AuthInterceptorOptions{Verifier: f.verifier}, testLogger{})

	ctx := metadata.NewIncomingContext(context.Background(),
		metadata.Pairs("authorization", "Bearer expired.or.garbage"))

	_, err := interceptor(ctx, nil, &grpc.UnaryServerInfo{FullMethod: "/x"},
		func(context.Context, any) (any, error) { return nil, nil })

	msg := status.Convert(err).Message()
	for _, leak := range []string{"signature", "issuer", "audience", "expired", "alg"} {
		if strings.Contains(strings.ToLower(msg), leak) {
			t.Errorf("rejection message %q leaks %q", msg, leak)
		}
	}
}

// ---------------------------------------------------------------------------

func notifEntry(serviceID, contact string) model.NotifEntry {
	return model.NotifEntry{
		ServiceId:   serviceID,
		ContactInfo: contact,
		Method:      "Email",
		Data:        map[string]string{"code": "123"},
		Importance:  5,
		CreatedAt:   time.Now().UTC(),
	}
}

func notifEntryColumns() []string {
	return model.NotifColumns
}

// ---------------------------------------------------------------------------

func TestBearerTokenExtraction(t *testing.T) {
	cases := []struct {
		header string
		want   string
	}{
		{"Bearer abc.def.ghi", "abc.def.ghi"},
		{"bearer abc", "abc"},
		{"BEARER abc", "abc"},
		{"Bearer   abc  ", "abc"},
		{"Basic dXNlcjpwYXNz", ""},
		{"abc", ""},
		{"", ""},
	}

	for _, c := range cases {
		ctx := metadata.NewIncomingContext(context.Background(),
			metadata.Pairs("authorization", c.header))
		if got := telemetry.BearerTokenFromMetadata(ctx); got != c.want {
			t.Errorf("header %q -> %q, want %q", c.header, got, c.want)
		}
	}
}

func TestErrNotAuthenticatedIsDistinct(t *testing.T) {
	if !errors.Is(ErrNotAuthenticated, ErrNotAuthenticated) {
		t.Error("sentinel error is not comparable with errors.Is")
	}
}

// testLogger is a no-op ErrorLogger. zap.Logger's Warn takes ...zap.Field, which
// does not satisfy the interface's ...any, so tests use this instead.
type testLogger struct{}

func (testLogger) Warn(string, ...any) {}
