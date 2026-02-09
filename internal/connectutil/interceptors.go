package connectutil

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"connectrpc.com/connect"
	"github.com/pitabwire/frame/security"
	connectInterceptors "github.com/pitabwire/frame/security/interceptors/connect"
	securityhttp "github.com/pitabwire/frame/security/interceptors/httptor"
)

// DefaultOptions returns the default Connect handler options (logging only, no auth).
// Use AuthenticatedOptions for production endpoints that require authentication.
func DefaultOptions() []connect.HandlerOption {
	return []connect.HandlerOption{
		connect.WithInterceptors(
			NewLoggingInterceptor(),
		),
	}
}

// AuthenticatedOptions returns Connect handler options with frame's full
// security interceptor chain (OpenTelemetry, validation, authentication)
// plus our logging interceptor.
func AuthenticatedOptions(ctx context.Context, authenticator security.Authenticator) ([]connect.HandlerOption, error) {
	interceptors, err := connectInterceptors.DefaultList(ctx, authenticator)
	if err != nil {
		return nil, err
	}
	// Append our logging interceptor after the auth chain.
	interceptors = append(interceptors, NewLoggingInterceptor())

	return []connect.HandlerOption{
		connect.WithInterceptors(interceptors...),
	}, nil
}

// AuthenticatedHTTPMiddleware wraps an http.Handler with frame's
// authentication middleware, validating bearer tokens on REST endpoints.
func AuthenticatedHTTPMiddleware(handler http.Handler, authenticator security.Authenticator) http.Handler {
	return securityhttp.AuthenticationMiddleware(handler, authenticator)
}

// DefaultClientOptions returns the default Connect client options.
func DefaultClientOptions() []connect.ClientOption {
	return []connect.ClientOption{
		connect.WithInterceptors(
			NewLoggingInterceptor(),
		),
	}
}

// --- Logging Interceptor (unary + streaming) ---

// loggingInterceptor implements connect.Interceptor for both unary and streaming RPCs.
type loggingInterceptor struct{}

// NewLoggingInterceptor creates an interceptor that logs RPC method, duration, and errors
// for both unary and streaming calls.
func NewLoggingInterceptor() connect.Interceptor {
	return &loggingInterceptor{}
}

func (l *loggingInterceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		start := time.Now()
		resp, err := next(ctx, req)
		duration := time.Since(start)

		attrs := []any{
			slog.String("procedure", req.Spec().Procedure),
			slog.Duration("duration", duration),
			slog.Bool("streaming", false),
		}

		if err != nil {
			attrs = append(attrs, slog.String("error", err.Error()))
			slog.WarnContext(ctx, "rpc error", attrs...)
		} else {
			slog.DebugContext(ctx, "rpc ok", attrs...)
		}

		return resp, err
	}
}

func (l *loggingInterceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return func(ctx context.Context, spec connect.Spec) connect.StreamingClientConn {
		slog.DebugContext(ctx, "rpc stream client start",
			slog.String("procedure", spec.Procedure),
			slog.Bool("streaming", true),
		)
		return next(ctx, spec)
	}
}

func (l *loggingInterceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return func(ctx context.Context, conn connect.StreamingHandlerConn) error {
		start := time.Now()
		slog.DebugContext(ctx, "rpc stream handler start",
			slog.String("procedure", conn.Spec().Procedure),
			slog.Bool("streaming", true),
		)

		err := next(ctx, conn)
		duration := time.Since(start)

		attrs := []any{
			slog.String("procedure", conn.Spec().Procedure),
			slog.Duration("duration", duration),
			slog.Bool("streaming", true),
		}

		if err != nil {
			attrs = append(attrs, slog.String("error", err.Error()))
			slog.WarnContext(ctx, "rpc stream error", attrs...)
		} else {
			slog.DebugContext(ctx, "rpc stream ok", attrs...)
		}

		return err
	}
}
