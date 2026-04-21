package handlers

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/go-orca/go-orca/internal/config"
	"github.com/go-orca/go-orca/internal/state"
	"github.com/go-orca/go-orca/internal/storage"
)

// defaultAllowedTypes is the default set of file extensions accepted for upload.
var defaultAllowedTypes = []string{
	".go", ".py", ".js", ".ts", ".jsx", ".tsx", ".rs", ".java", ".c", ".cpp", ".h",
	".md", ".txt", ".json", ".yaml", ".yml", ".toml", ".xml", ".csv",
	".html", ".css", ".scss", ".sql", ".sh", ".bash", ".zsh",
	".proto", ".graphql", ".tf", ".hcl",
	".dockerfile", ".makefile",
}

// ─── Upload session handlers ──────────────────────────────────────────────────

// CreateUploadSession handles POST /upload-sessions.
func CreateUploadSession(store storage.Store, cfg config.IngestionConfig, log *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		tid := tenantID(c)
		sid := scopeID(c)
		if tid == "" || sid == "" {
			respondError(c, http.StatusBadRequest, "X-Tenant-ID and X-Scope-ID headers are required")
			return
		}

		ttl := cfg.UploadSessionTTL
		if ttl == 0 {
			ttl = 30 * time.Minute
		}

		now := time.Now().UTC()
		sess := &state.UploadSession{
			ID:        uuid.New().String(),
			TenantID:  tid,
			ScopeID:   sid,
			Status:    state.UploadSessionOpen,
			ExpiresAt: now.Add(ttl),
			CreatedAt: now,
			UpdatedAt: now,
		}

		if err := store.CreateUploadSession(c.Request.Context(), sess); err != nil {
			log.Error("create upload session", zap.Error(err))
			respondError(c, http.StatusInternalServerError, "failed to create upload session")
			return
		}

		c.JSON(http.StatusCreated, sess)
	}
}

// UploadFile handles POST /upload-sessions/:sessionId/files.
// Accepts a multipart file upload and persists it to disk.
func UploadFile(store storage.Store, cfg config.IngestionConfig, storagePath string, log *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		sessionID := c.Param("sessionId")
		tid := tenantID(c)
		sid := scopeID(c)
		if tid == "" || sid == "" {
			respondError(c, http.StatusBadRequest, "X-Tenant-ID and X-Scope-ID headers are required")
			return
		}

		// Validate session ownership and status.
		sess, err := store.GetUploadSession(c.Request.Context(), sessionID)
		if err != nil {
			respondError(c, http.StatusNotFound, "upload session not found")
			return
		}
		if sess.TenantID != tid {
			respondError(c, http.StatusForbidden, "upload session does not belong to this tenant")
			return
		}
		if sess.Status != state.UploadSessionOpen {
			respondError(c, http.StatusConflict, fmt.Sprintf("upload session is %s", sess.Status))
			return
		}
		if time.Now().UTC().After(sess.ExpiresAt) {
			respondError(c, http.StatusGone, "upload session has expired")
			return
		}

		// Parse multipart file.
		file, header, err := c.Request.FormFile("file")
		if err != nil {
			respondError(c, http.StatusBadRequest, "missing file field in multipart form")
			return
		}
		defer file.Close()

		// Extract relative path if provided (for folder uploads).
		relativePath := c.PostForm("relative_path")
		if relativePath == "" {
			// Fall back to just the filename for backwards compatibility.
			relativePath = filepath.Base(header.Filename)
		}

		// Validate file size.
		maxSize := cfg.MaxFileSize
		if maxSize == 0 {
			maxSize = 10 * 1024 * 1024 // 10 MB default
		}
		if header.Size > maxSize {
			respondError(c, http.StatusRequestEntityTooLarge,
				fmt.Sprintf("file exceeds maximum size of %d bytes", maxSize))
			return
		}

		// Validate file extension.
		ext := strings.ToLower(filepath.Ext(header.Filename))
		allowed := cfg.AllowedTypes
		if len(allowed) == 0 {
			allowed = defaultAllowedTypes
		}
		extAllowed := false
		for _, a := range allowed {
			if ext == a {
				extAllowed = true
				break
			}
		}
		if !extAllowed {
			respondError(c, http.StatusUnsupportedMediaType,
				fmt.Sprintf("file type %q is not supported", ext))
			return
		}

		// Persist file to disk.
		attID := uuid.New().String()
		dir := filepath.Join(storagePath, "attachments", sessionID)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			log.Error("create attachment dir", zap.Error(err))
			respondError(c, http.StatusInternalServerError, "failed to store file")
			return
		}
		// Use attID as filename on disk to avoid path traversal.
		diskPath := filepath.Join(dir, attID+ext)
		dst, err := os.Create(diskPath)
		if err != nil {
			log.Error("create attachment file", zap.Error(err))
			respondError(c, http.StatusInternalServerError, "failed to store file")
			return
		}
		written, err := io.Copy(dst, file)
		dst.Close()
		if err != nil {
			os.Remove(diskPath)
			log.Error("write attachment file", zap.Error(err))
			respondError(c, http.StatusInternalServerError, "failed to store file")
			return
		}

		now := time.Now().UTC()
		att := &state.Attachment{
			ID:              attID,
			UploadSessionID: sessionID,
			TenantID:        tid,
			ScopeID:         sid,
			Filename:        filepath.Base(header.Filename),
			RelativePath:    relativePath,
			ContentType:     header.Header.Get("Content-Type"),
			SizeBytes:       written,
			StoragePath:     diskPath,
			Status:          state.AttachmentPending,
			CreatedAt:       now,
			UpdatedAt:       now,
		}

		if err := store.CreateAttachment(c.Request.Context(), att); err != nil {
			os.Remove(diskPath)
			log.Error("persist attachment metadata", zap.Error(err))
			respondError(c, http.StatusInternalServerError, "failed to persist attachment")
			return
		}

		c.JSON(http.StatusCreated, att)
	}
}

// ListSessionFiles handles GET /upload-sessions/:sessionId/files.
func ListSessionFiles(store storage.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		sessionID := c.Param("sessionId")
		tid := tenantID(c)

		sess, err := store.GetUploadSession(c.Request.Context(), sessionID)
		if err != nil {
			respondError(c, http.StatusNotFound, "upload session not found")
			return
		}
		if tid != "" && sess.TenantID != tid {
			respondError(c, http.StatusForbidden, "upload session does not belong to this tenant")
			return
		}

		atts, err := store.ListAttachmentsBySession(c.Request.Context(), sessionID)
		if err != nil {
			respondError(c, http.StatusInternalServerError, "failed to list attachments")
			return
		}
		c.JSON(http.StatusOK, atts)
	}
}

// AbortUploadSession handles POST /upload-sessions/:sessionId/abort.
func AbortUploadSession(store storage.Store, log *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		sessionID := c.Param("sessionId")
		tid := tenantID(c)
		if tid == "" {
			respondError(c, http.StatusBadRequest, "X-Tenant-ID header is required")
			return
		}

		if err := store.AbortUploadSession(c.Request.Context(), sessionID, tid); err != nil {
			log.Error("abort upload session", zap.String("session_id", sessionID), zap.Error(err))
			respondError(c, http.StatusBadRequest, err.Error())
			return
		}

		c.JSON(http.StatusOK, gin.H{"status": "aborted"})
	}
}

// ListWorkflowAttachments handles GET /workflows/:id/attachments.
func ListWorkflowAttachments(store storage.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		ws, ok := checkWorkflowOwnership(c, store)
		if !ok {
			return
		}
		atts, err := store.ListAttachmentsByWorkflow(c.Request.Context(), ws.ID)
		if err != nil {
			respondError(c, http.StatusInternalServerError, "failed to list attachments")
			return
		}
		c.JSON(http.StatusOK, atts)
	}
}
