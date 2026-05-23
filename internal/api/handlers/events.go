package handlers

import (
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// EventProducer enqueues per-user events to a streaming backend.
type EventProducer interface {
	Produce(userID string, payload []byte) error
	Topic() string
}

// HTTPResponseObserver tracks status codes for the ingest endpoint.
type HTTPResponseObserver interface {
	ObserveHTTPResponse(code int)
}

// IngestEvent handles POST /api/v1/events.
func IngestEvent(producer EventProducer, obs HTTPResponseObserver, log *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		track := func(code int) {
			if obs != nil {
				obs.ObserveHTTPResponse(code)
			}
		}
		if producer == nil {
			track(http.StatusServiceUnavailable)
			respondError(c, http.StatusServiceUnavailable, "streaming producer unavailable")
			return
		}

		userID := strings.TrimSpace(c.GetString("mesh_user_id"))
		if userID == "" {
			track(http.StatusBadRequest)
			respondError(c, http.StatusBadRequest, "missing required user identity header")
			return
		}

		payload, err := io.ReadAll(c.Request.Body)
		if err != nil {
			track(http.StatusBadRequest)
			respondError(c, http.StatusBadRequest, "failed to read request body")
			return
		}
		if len(payload) == 0 {
			track(http.StatusBadRequest)
			respondError(c, http.StatusBadRequest, "request body must not be empty")
			return
		}

		if err := producer.Produce(userID, payload); err != nil {
			track(http.StatusServiceUnavailable)
			respondError(c, http.StatusServiceUnavailable, "failed to enqueue event")
			return
		}

		log.Info("streaming event accepted",
			zap.String("user_id", userID),
			zap.String("topic", producer.Topic()),
			zap.Int("payload_bytes", len(payload)),
		)
		track(http.StatusAccepted)
		c.JSON(http.StatusAccepted, gin.H{"status": "accepted"})
	}
}
