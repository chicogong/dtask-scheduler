package middleware_test

import (
	"bytes"
	"context"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chicogong/dtask-scheduler/pkg/middleware"
)

// ---------- helpers ----------

func newLogger(buf *bytes.Buffer) *log.Logger {
	return log.New(buf, "", 0)
}

// ---------- Recover ----------

func TestRecover_Panic(t *testing.T) {
	var buf bytes.Buffer
	logger := newLogger(&buf)

	h := middleware.Recover(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("something went wrong")
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("Recover panic: got status %d, want %d", rec.Code, http.StatusInternalServerError)
	}
	if !strings.Contains(buf.String(), "something went wrong") {
		t.Errorf("Recover panic: logger output %q does not contain panic value", buf.String())
	}
}

func TestRecover_NilLogger_UsesDefault(t *testing.T) {
	// Must not panic when logger is nil.
	h := middleware.Recover(nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("boom")
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("Recover nil logger: got status %d, want 500", rec.Code)
	}
}

func TestRecover_NoPanic(t *testing.T) {
	var buf bytes.Buffer
	logger := newLogger(&buf)

	h := middleware.Recover(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))

	if rec.Code != http.StatusOK {
		t.Errorf("Recover no-panic: got status %d, want 200", rec.Code)
	}
	if buf.Len() != 0 {
		t.Errorf("Recover no-panic: unexpected log output: %q", buf.String())
	}
}

// ---------- AccessLog ----------

func TestAccessLog_LogsMethodPathStatus(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		path       string
		handler    http.HandlerFunc
		wantStatus int
	}{
		{
			name:   "GET 200",
			method: http.MethodGet,
			path:   "/tasks",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			},
			wantStatus: http.StatusOK,
		},
		{
			name:   "POST 201",
			method: http.MethodPost,
			path:   "/tasks/create",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusCreated)
			},
			wantStatus: http.StatusCreated,
		},
		{
			name:   "GET 404",
			method: http.MethodGet,
			path:   "/not-found",
			handler: func(w http.ResponseWriter, r *http.Request) {
				http.NotFound(w, r)
			},
			wantStatus: http.StatusNotFound,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			logger := newLogger(&buf)

			h := middleware.AccessLog(logger)(tc.handler)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, nil))

			out := buf.String()
			if !strings.Contains(out, tc.method) {
				t.Errorf("AccessLog: output %q missing method %q", out, tc.method)
			}
			if !strings.Contains(out, tc.path) {
				t.Errorf("AccessLog: output %q missing path %q", out, tc.path)
			}
			wantCode := strings.Fields(out) // find the status code anywhere
			found := false
			for _, f := range wantCode {
				if f == http.StatusText(tc.wantStatus) || strings.Contains(f, "20") || strings.HasPrefix(f, "4") {
					// Just verify status int is somewhere
					found = true
					break
				}
			}
			_ = found // We verify by checking the raw integer below.
			statusStr := ""
			switch tc.wantStatus {
			case 200:
				statusStr = "200"
			case 201:
				statusStr = "201"
			case 404:
				statusStr = "404"
			}
			if !strings.Contains(out, statusStr) {
				t.Errorf("AccessLog: output %q missing status %s", out, statusStr)
			}
		})
	}
}

func TestAccessLog_NilLogger(t *testing.T) {
	// Must not panic when logger is nil.
	h := middleware.AccessLog(nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	rec := httptest.NewRecorder()
	// Should not panic.
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
}

// ---------- BodyLimit ----------

func TestBodyLimit_Exceeded(t *testing.T) {
	const limit = 10

	var readErr error
	h := middleware.BodyLimit(limit)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, readErr = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))

	body := strings.NewReader(strings.Repeat("x", limit+1))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/upload", body))

	if readErr == nil {
		t.Error("BodyLimit exceeded: expected read error, got nil")
	}
}

func TestBodyLimit_WithinLimit(t *testing.T) {
	const limit = 100
	const payload = "hello"

	var (
		readErr  error
		readData []byte
	)
	h := middleware.BodyLimit(limit)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		readData, readErr = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/upload", strings.NewReader(payload)))

	if readErr != nil {
		t.Errorf("BodyLimit within limit: unexpected read error: %v", readErr)
	}
	if string(readData) != payload {
		t.Errorf("BodyLimit within limit: got %q, want %q", readData, payload)
	}
}

// ---------- RequestID ----------

func TestRequestID_HeaderPresent(t *testing.T) {
	h := middleware.RequestID()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	id := rec.Header().Get("X-Request-ID")
	if id == "" {
		t.Error("RequestID: X-Request-ID header is empty")
	}
}

func TestRequestID_UniqueAcrossRequests(t *testing.T) {
	h := middleware.RequestID()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	ids := make(map[string]struct{}, 10)
	for i := 0; i < 10; i++ {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
		id := rec.Header().Get("X-Request-ID")
		if _, dup := ids[id]; dup {
			t.Errorf("RequestID: duplicate ID %q generated", id)
		}
		ids[id] = struct{}{}
	}
}

func TestRequestID_ContextValue(t *testing.T) {
	var ctxID string
	h := middleware.RequestID()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctxID = middleware.RequestIDFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	headerID := rec.Header().Get("X-Request-ID")
	if headerID == "" {
		t.Fatal("RequestID: X-Request-ID header is empty")
	}
	if ctxID != headerID {
		t.Errorf("RequestID: context value %q != header value %q", ctxID, headerID)
	}
}

func TestRequestIDFromContext_Missing(t *testing.T) {
	id := middleware.RequestIDFromContext(context.Background())
	if id != "" {
		t.Errorf("RequestIDFromContext missing: got %q, want empty string", id)
	}
}

// ---------- Chain ----------

func TestChain_Order(t *testing.T) {
	// Each middleware appends its marker to events before and after calling next.
	var events []string

	makeMarker := func(label string) middleware.Middleware {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				events = append(events, label+":before")
				next.ServeHTTP(w, r)
				events = append(events, label+":after")
			})
		}
	}

	h := middleware.Chain(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			events = append(events, "handler")
		}),
		makeMarker("A"),
		makeMarker("B"),
	)

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	want := []string{"A:before", "B:before", "handler", "B:after", "A:after"}
	if len(events) != len(want) {
		t.Fatalf("Chain order: got events %v, want %v", events, want)
	}
	for i, e := range events {
		if e != want[i] {
			t.Errorf("Chain order[%d]: got %q, want %q", i, e, want[i])
		}
	}
}

func TestChain_NoMiddleware(t *testing.T) {
	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})

	h := middleware.Chain(inner)
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	if !called {
		t.Error("Chain with no middleware: inner handler was not called")
	}
}

// ---------- responseWriter (internal behaviour via AccessLog) ----------

func TestResponseWriter_DefaultStatus200(t *testing.T) {
	// A handler that never calls WriteHeader — AccessLog should record 200.
	var buf bytes.Buffer
	logger := newLogger(&buf)

	h := middleware.AccessLog(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Intentionally write body without calling WriteHeader.
		_, _ = io.WriteString(w, "ok")
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ping", nil))

	if !strings.Contains(buf.String(), "200") {
		t.Errorf("responseWriter default status: log output %q does not contain 200", buf.String())
	}
}

func TestResponseWriter_ByteCount(t *testing.T) {
	const body = "hello world"
	var buf bytes.Buffer
	logger := newLogger(&buf)

	h := middleware.AccessLog(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, body)
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	// The log line should contain the byte count.
	expectedBytes := "11" // len("hello world")
	if !strings.Contains(buf.String(), expectedBytes) {
		t.Errorf("responseWriter byte count: log output %q does not contain %q", buf.String(), expectedBytes)
	}
}
