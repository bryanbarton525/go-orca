package handlers_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/go-orca/go-orca/internal/api/handlers"
)

type fakeEventProducer struct {
	err      error
	lastUser string
	lastBody string
}

func (f *fakeEventProducer) Produce(userID string, payload []byte) error {
	f.lastUser = userID
	f.lastBody = string(payload)
	return f.err
}

func (f *fakeEventProducer) Topic() string { return "orca.events" }

type fakeHTTPObserver struct{ codes []int }

func (f *fakeHTTPObserver) ObserveHTTPResponse(code int) { f.codes = append(f.codes, code) }

func TestIngestEventAccepted(t *testing.T) {
	gin.SetMode(gin.TestMode)
	producer := &fakeEventProducer{}
	observer := &fakeHTTPObserver{}

	r := gin.New()
	r.POST("/api/v1/events", func(c *gin.Context) {
		c.Set("mesh_user_id", "user-1")
		handlers.IngestEvent(producer, observer, zap.NewNop())(c)
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/events", strings.NewReader(`{"hello":"world"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", w.Code)
	}
	if producer.lastUser != "user-1" {
		t.Fatalf("expected user-1, got %q", producer.lastUser)
	}
	if producer.lastBody == "" {
		t.Fatal("expected non-empty payload")
	}
	if len(observer.codes) != 1 || observer.codes[0] != http.StatusAccepted {
		t.Fatalf("unexpected metrics codes: %+v", observer.codes)
	}
}

func TestIngestEventMissingUserID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	producer := &fakeEventProducer{}
	observer := &fakeHTTPObserver{}

	r := gin.New()
	r.POST("/api/v1/events", handlers.IngestEvent(producer, observer, zap.NewNop()))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/events", strings.NewReader(`{"hello":"world"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	if len(observer.codes) != 1 || observer.codes[0] != http.StatusBadRequest {
		t.Fatalf("unexpected metrics codes: %+v", observer.codes)
	}
}
