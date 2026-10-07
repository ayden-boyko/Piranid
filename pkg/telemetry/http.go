package telemetry

import (
	"context"
	"net/http"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// bearerPrefix is the Authorization scheme, per RFC 6750.
const bearerPrefix = "Bearer "

// authHeader is the metadata key carrying a caller's credential.
const authHeader = "authorization"

// HTTPMiddleware wraps an http.Handler so every request produces a server span.
//
// This is a small stand-in for otelhttp. The upstream package pulls a large
// dependency subtree and forced an OpenTelemetry SDK upgrade that broke
// pkg/telemetry, which is a poor trade on hardware with a memory budget.
//
// What it does that the services previously did not:
//
//   - starts a span from the incoming traceparent header, so an upstream trace
//     is continued rather than orphaned
//   - records the status code and method as span attributes
//   - marks the span as an error on a 5xx
//
// Handlers should derive their own spans from r.Context(), never from a context
// captured at startup. Event_Queue did the latter, so client traces never
// connected to its spans.
func HTTPMiddleware(service string, next http.Handler) http.Handler {
	tracer := otel.Tracer(service)
	propagator := otel.GetTextMapPropagator()

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := propagator.Extract(r.Context(), propagation.HeaderCarrier(r.Header))

		ctx, span := tracer.Start(ctx, r.Method+" "+r.URL.Path,
			trace.WithSpanKind(trace.SpanKindServer),
			trace.WithAttributes(
				attribute.String("http.request.method", r.Method),
				attribute.String("url.path", r.URL.Path),
				attribute.String("server.address", r.Host),
			),
		)
		defer span.End()

		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r.WithContext(ctx))

		span.SetAttributes(attribute.Int("http.response.status_code", rec.status))
		if rec.status >= 500 {
			span.SetStatus(codes.Error, http.StatusText(rec.status))
		}
	})
}

// statusRecorder captures the status code a handler writes.
type statusRecorder struct {
	http.ResponseWriter
	status  int
	written bool
}

func (r *statusRecorder) WriteHeader(status int) {
	if r.written {
		// Swallow a second WriteHeader. Handlers that wrote 200 before deciding
		// to fail used to emit Go's "superfluous WriteHeader" warning and send a
		// misleading status.
		return
	}
	r.status = status
	r.written = true
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if !r.written {
		r.WriteHeader(http.StatusOK)
	}
	return r.ResponseWriter.Write(b)
}

// Flush keeps streaming handlers working through the wrapper.
func (r *statusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// UnaryServerInterceptor starts a span for each unary RPC and extracts the
// caller's trace context from metadata.
//
// Notifications had no interceptor at all, so its spans were islands and its
// metrics and traces could not be joined to a caller's.
func UnaryServerInterceptor(service string) grpc.UnaryServerInterceptor {
	tracer := otel.Tracer(service)
	propagator := otel.GetTextMapPropagator()

	return func(
		ctx context.Context,
		req any,
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (any, error) {
		if md, ok := metadata.FromIncomingContext(ctx); ok {
			ctx = propagator.Extract(ctx, metadataCarrier(md))
		}

		ctx, span := tracer.Start(ctx, info.FullMethod,
			trace.WithSpanKind(trace.SpanKindServer),
			trace.WithAttributes(attribute.String("rpc.system", "grpc"),
				attribute.String("rpc.method", info.FullMethod)),
		)
		defer span.End()

		resp, err := handler(ctx, req)
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		} else {
			span.SetStatus(codes.Ok, "")
		}
		return resp, err
	}
}

// StreamServerInterceptor is the streaming counterpart of
// UnaryServerInterceptor.
func StreamServerInterceptor(service string) grpc.StreamServerInterceptor {
	tracer := otel.Tracer(service)
	propagator := otel.GetTextMapPropagator()

	return func(
		srv any,
		stream grpc.ServerStream,
		info *grpc.StreamServerInfo,
		handler grpc.StreamHandler,
	) error {
		ctx := stream.Context()
		if md, ok := metadata.FromIncomingContext(ctx); ok {
			ctx = propagator.Extract(ctx, metadataCarrier(md))
		}

		ctx, span := tracer.Start(ctx, info.FullMethod,
			trace.WithSpanKind(trace.SpanKindServer),
			trace.WithAttributes(attribute.String("rpc.system", "grpc"),
				attribute.String("rpc.method", info.FullMethod)),
		)
		defer span.End()

		err := handler(srv, &wrappedStream{ServerStream: stream, ctx: ctx})
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		} else {
			span.SetStatus(codes.Ok, "")
		}
		return err
	}
}

type wrappedStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (w *wrappedStream) Context() context.Context { return w.ctx }

// BearerTokenFromMetadata extracts a bearer token from incoming gRPC metadata.
//
// Callers that omit the credential get "", which the auth interceptor treats as
// unauthenticated. The scheme comparison is case-insensitive per RFC 7235.
func BearerTokenFromMetadata(ctx context.Context) string {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ""
	}
	values := md.Get(authHeader)
	if len(values) == 0 {
		return ""
	}
	return TrimBearer(values[0])
}

// TrimBearer strips the Bearer scheme prefix, returning "" if absent.
func TrimBearer(header string) string {
	if len(header) >= len(bearerPrefix) &&
		strings.EqualFold(header[:len(bearerPrefix)], bearerPrefix) {
		return strings.TrimSpace(header[len(bearerPrefix):])
	}
	return ""
}

// metadataCarrier adapts gRPC metadata to the OTel propagator's carrier.
type metadataCarrier metadata.MD

// Get returns the first value for a key, as the OTel propagator requires.
//
// It indexes the map directly rather than calling metadata.MD.Get, because MD is
// a map type and the promoted method does not resolve unambiguously here.
func (mc metadataCarrier) Get(key string) string {
	values := map[string][]string(mc)[key]
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func (mc metadataCarrier) Set(key string, value string) {
	metadata.MD(mc).Set(key, value)
}

func (mc metadataCarrier) Keys() []string {
	out := make([]string, 0, len(mc))
	for k := range mc {
		out = append(out, k)
	}
	return out
}
