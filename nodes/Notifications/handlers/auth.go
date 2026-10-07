package handlers

import (
	"context"
	"errors"
	"strings"

	"Piranid/pkg/authn"
	"Piranid/pkg/telemetry"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// AuthInterceptorOptions configures bearer verification for the gRPC server.
//
// The notification server was constructed with grpc.NewServer() and no options,
// so every RPC was reachable without a credential. DeleteUser is destructive and
// RequestTFA would carry a contact address, so this is not a formality.
type AuthInterceptorOptions struct {
	// Verifier checks the token. Nil disables verification.
	Verifier *authn.Verifier

	// JWKSURL is fetched lazily to build a verifier when Verifier is nil.
	JWKSURL string

	// RequiredScopes must all be present on the token, when non-empty.
	RequiredScopes []string

	// Issuer and Audience are the expected iss and aud claims. Both must match
	// the auth node's configuration.
	Issuer   string
	Audience string

	// AllowAnonymous passes requests through without a credential.
	//
	// Development only. It logs a warning through the returned interceptor's
	// caller and must never be enabled in a deployment.
	AllowAnonymous bool
}

// ErrorLogger is the minimal logging surface the interceptor needs.
type ErrorLogger interface {
	Warn(msg string, fields ...any)
}

// UnaryAuthInterceptor returns a gRPC unary interceptor that verifies a bearer
// token from request metadata.
//
// Verified claims are placed in the context, retrievable with ClaimsFrom, so
// handlers can authorise on service identity rather than trusting the
// service_id in the request body.
func UnaryAuthInterceptor(opts AuthInterceptorOptions, logger ErrorLogger) grpc.UnaryServerInterceptor {
	verifier := opts.Verifier

	if verifier == nil && !opts.AllowAnonymous {
		if opts.JWKSURL == "" {
			logger.Warn("token verification is NOT configured: AUTH_JWKS_URL is " +
				"unset. Every RPC will be rejected rather than served unauthenticated.")
		} else {
			fetcher := authn.NewJWKSFetcher(opts.JWKSURL, nil, 0)
			// A failure here is not fatal: the fetcher retries on the first
			// unknown kid, so a transient outage at startup does not permanently
			// disable the node.
			if err := fetcher.Warm(); err != nil {
				logger.Warn("could not fetch JWKS at startup, will retry on first use",
					"url", opts.JWKSURL, "error", err.Error())
			}
			verifier = authn.NewVerifier(fetcher,
				authn.WithIssuer(opts.Issuer),
				authn.WithAudience(opts.Audience),
			)
		}
	}

	if opts.AllowAnonymous {
		logger.Warn("AUTH_ALLOW_ANONYMOUS is set: token verification is DISABLED")
	}

	return func(
		ctx context.Context,
		req any,
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (any, error) {
		if opts.AllowAnonymous || verifier == nil {
			// Fail closed: an unconfigured verifier rejects rather than admits.
			if !opts.AllowAnonymous && verifier == nil {
				return nil, status.Error(codes.Internal,
					"token verification is not configured on this server")
			}
			return handler(ctx, req)
		}

		token := telemetry.BearerTokenFromMetadata(ctx)
		if token == "" {
			return nil, status.Error(codes.Unauthenticated,
				"a Bearer access token is required")
		}

		claims, err := verifier.Verify(token)
		if err != nil {
			// The reason is logged but not returned: distinguishing "expired"
			// from "bad signature" in an unauthenticated response is a free
			// oracle for probing tokens.
			logger.Warn("gRPC token verification failed",
				"method", info.FullMethod, "error", err.Error())
			return nil, status.Error(codes.Unauthenticated, "the access token is not valid")
		}

		for _, required := range opts.RequiredScopes {
			if !claims.HasScope(required) {
				return nil, status.Error(codes.PermissionDenied,
					"the token lacks a required scope")
			}
		}

		return handler(authn.WithClaims(ctx, claims), req)
	}
}

// StreamAuthInterceptor is the streaming counterpart.
//
// The notifier service declares no streaming RPCs today; this exists so adding
// one does not silently skip verification.
func StreamAuthInterceptor(opts AuthInterceptorOptions, logger ErrorLogger) grpc.StreamServerInterceptor {
	unary := UnaryAuthInterceptor(opts, logger)

	return func(
		srv any,
		stream grpc.ServerStream,
		info *grpc.StreamServerInfo,
		handler grpc.StreamHandler,
	) error {
		if opts.AllowAnonymous {
			return handler(srv, stream)
		}

		ctx := stream.Context()
		if telemetry.BearerTokenFromMetadata(ctx) == "" {
			return status.Error(codes.Unauthenticated, "a Bearer access token is required")
		}

		// Reuse the unary path for the credential check by wrapping the stream
		// handler.
		_, err := unary(ctx, nil, &grpc.UnaryServerInfo{
			Server:     srv,
			FullMethod: info.FullMethod,
		}, func(c context.Context, _ any) (any, error) {
			return nil, handler(srv, &wrappedStream{ServerStream: stream, ctx: c})
		})
		return err
	}
}

type wrappedStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (w *wrappedStream) Context() context.Context { return w.ctx }

// ClaimsFrom retrieves verified claims from a handler's context.
func ClaimsFrom(ctx context.Context) (*authn.Claims, bool) {
	return authn.ClaimsFrom(ctx)
}

// CallerServiceID returns the client_id from the verified token.
//
// Callers should prefer this over a service_id supplied in a request body: the
// body is caller-controlled, so trusting it would let one service impersonate
// another.
func CallerServiceID(ctx context.Context) (string, bool) {
	claims, ok := authn.ClaimsFrom(ctx)
	if !ok {
		return "", false
	}
	if claims.ClientID == "" {
		return "", false
	}
	return claims.ClientID, true
}

// ErrNotAuthenticated reports a missing verified credential.
var ErrNotAuthenticated = errors.New("notifications: caller is not authenticated")

// bearerFromMetadata is retained for tests that construct metadata by hand.
func bearerFromMetadata(ctx context.Context) string {
	v := telemetry.BearerTokenFromMetadata(ctx)
	return strings.TrimSpace(v)
}
