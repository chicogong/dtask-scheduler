// Package middleware provides composable net/http middleware for the dtask-scheduler.
//
// Middlewares can be composed via [Chain], which layers them so the first
// argument is the outermost (first to see an incoming request):
//
//	handler := middleware.Chain(
//	    mux,
//	    middleware.Recover(logger),
//	    middleware.AccessLog(logger),
//	    middleware.BodyLimit(1<<20),
//	    middleware.RequestID(),
//	)
package middleware

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net/http"
	"runtime/debug"
	"time"
)

// Middleware is a function that wraps an [http.Handler] and returns a new
// [http.Handler], allowing cross-cutting concerns to be composed around a
// core handler.
type Middleware func(http.Handler) http.Handler

// Chain composes middlewares around h so that the FIRST middleware listed is
// the OUTERMOST layer — i.e. it receives the request first. If no middlewares
// are provided, h is returned unchanged.
//
// Example:
//
//	h := middleware.Chain(mux, middleware.Recover(logger), middleware.AccessLog(logger))
func Chain(h http.Handler, mws ...Middleware) http.Handler {
	// Apply in reverse order so the first element ends up outermost.
	for i := len(mws) - 1; i >= 0; i-- {
		h = mws[i](h)
	}
	return h
}

// responseWriter is an [http.ResponseWriter] wrapper that captures the HTTP
// status code and the number of bytes written to the response body.
// When [http.ResponseWriter.WriteHeader] is never called it defaults to 200.
type responseWriter struct {
	http.ResponseWriter
	status       int
	bytesWritten int
	wroteHeader  bool
}

// newResponseWriter wraps w with status-capturing behaviour.
func newResponseWriter(w http.ResponseWriter) *responseWriter {
	return &responseWriter{ResponseWriter: w, status: http.StatusOK}
}

// WriteHeader records the status code and delegates to the underlying writer.
func (rw *responseWriter) WriteHeader(code int) {
	if rw.wroteHeader {
		return
	}
	rw.status = code
	rw.wroteHeader = true
	rw.ResponseWriter.WriteHeader(code)
}

// Write writes data to the response body and accumulates the byte count.
func (rw *responseWriter) Write(b []byte) (int, error) {
	if !rw.wroteHeader {
		// net/http does this implicitly; mirror it so status is recorded.
		rw.WriteHeader(http.StatusOK)
	}
	n, err := rw.ResponseWriter.Write(b)
	rw.bytesWritten += n
	return n, err
}

// Status returns the HTTP status code that was written (defaults to 200).
func (rw *responseWriter) Status() int {
	return rw.status
}

// BytesWritten returns the total number of bytes written to the response body.
func (rw *responseWriter) BytesWritten() int {
	return rw.bytesWritten
}

// Recover returns a [Middleware] that recovers from panics in downstream
// handlers. The panic value and stack trace are logged via logger; if logger
// is nil, [log.Default] is used. If no response has been written yet the
// middleware writes 500 Internal Server Error.
func Recover(logger *log.Logger) Middleware {
	if logger == nil {
		logger = log.Default()
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rw := newResponseWriter(w)
			defer func() {
				if rec := recover(); rec != nil {
					logger.Printf("panic recovered: %v\n%s", rec, debug.Stack())
					if !rw.wroteHeader {
						http.Error(rw, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
					}
				}
			}()
			next.ServeHTTP(rw, r)
		})
	}
}

// AccessLog returns a [Middleware] that logs one line per request after the
// handler returns. The log line includes the HTTP method, request path,
// response status code, response size in bytes, and elapsed duration.
// If logger is nil, [log.Default] is used.
func AccessLog(logger *log.Logger) Middleware {
	if logger == nil {
		logger = log.Default()
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rw := newResponseWriter(w)
			start := time.Now()
			next.ServeHTTP(rw, r)
			logger.Printf("%s %s %d %d %s",
				r.Method,
				r.URL.Path,
				rw.Status(),
				rw.BytesWritten(),
				time.Since(start),
			)
		})
	}
}

// BodyLimit returns a [Middleware] that restricts the request body to at most
// maxBytes bytes. Downstream handlers that attempt to read beyond the limit
// will receive an error from the [io.Reader]. The limit is enforced via
// [http.MaxBytesReader].
func BodyLimit(maxBytes int64) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
			next.ServeHTTP(w, r)
		})
	}
}

// contextKey is an unexported type used as a key in [context.Context] values
// set by this package, preventing collisions with other packages.
type contextKey int

const requestIDKey contextKey = iota

// RequestID returns a [Middleware] that assigns each request a unique,
// hex-encoded ID generated from crypto/rand. The ID is stored in the request
// context (retrievable via [RequestIDFromContext]) and is also set as the
// X-Request-ID response header.
func RequestID() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id, err := generateID()
			if err != nil {
				// Extremely unlikely, but fall back gracefully.
				id = fmt.Sprintf("err-%d", time.Now().UnixNano())
			}
			w.Header().Set("X-Request-ID", id)
			ctx := context.WithValue(r.Context(), requestIDKey, id)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequestIDFromContext retrieves the request ID stored by [RequestID] from ctx.
// It returns an empty string if no ID is present.
func RequestIDFromContext(ctx context.Context) string {
	v, _ := ctx.Value(requestIDKey).(string)
	return v
}

// generateID returns a 16-byte (128-bit) cryptographically random hex string.
func generateID() (string, error) {
	b := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
