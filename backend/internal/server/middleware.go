package server

import (
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/m2sound2456/staffdisplay/backend/internal/logger"
)

// RequestIDHeader is sent back to clients so a browser error can be matched with
// a server log line.
const RequestIDHeader = "X-Request-ID"

// requestIDKey is the gin context key holding the current request id.
const requestIDKey = "request_id"

// maxRequestIDLength protects the log pipeline from oversized client values.
const maxRequestIDLength = 128

// RequestID reads or generates the request correlation id.
func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		requestID := strings.TrimSpace(c.GetHeader(RequestIDHeader))
		if requestID == "" || len(requestID) > maxRequestIDLength {
			requestID = uuid.NewString()
		}
		c.Set(requestIDKey, requestID)
		c.Header(RequestIDHeader, requestID)
		c.Next()
	}
}

// RequestIDOf returns the request id stored on the context ("" when absent).
func RequestIDOf(c *gin.Context) string {
	if c == nil {
		return ""
	}
	if value, ok := c.Get(requestIDKey); ok {
		if id, ok := value.(string); ok {
			return id
		}
	}
	return ""
}

// AccessLog writes one structured log line per request.
func AccessLog() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		query := c.Request.URL.RawQuery

		c.Next()

		if query != "" {
			path = path + "?" + query
		}
		status := c.Writer.Status()
		fields := []zap.Field{
			zap.String("request_id", RequestIDOf(c)),
			zap.String("method", c.Request.Method),
			zap.String("path", path),
			zap.Int("status", status),
			zap.Int("bytes", c.Writer.Size()),
			zap.Duration("latency", time.Since(start)),
			zap.String("client_ip", c.ClientIP()),
		}
		switch {
		case status >= http.StatusInternalServerError:
			logger.Error("http_request", fields...)
		case status >= http.StatusBadRequest:
			logger.Warn("http_request", fields...)
		default:
			logger.Info("http_request", fields...)
		}
	}
}

// Recovery converts a panic in any handler into a 500 error envelope and logs
// the stack trace, so one bad request can never take the API down.
func Recovery() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if recovered := recover(); recovered != nil {
				logger.Error("http_panic_recovered",
					zap.String("request_id", RequestIDOf(c)),
					zap.String("method", c.Request.Method),
					zap.String("path", c.Request.URL.Path),
					zap.Any("panic", recovered),
					zap.ByteString("stack", debug.Stack()),
				)
				Fail(c, http.StatusInternalServerError, CodeInternalError, "internal server error", nil)
			}
		}()
		c.Next()
	}
}

// SecurityHeaders sets conservative defaults; nginx repeats them in production.
func SecurityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		headers := c.Writer.Header()
		headers.Set("X-Content-Type-Options", "nosniff")
		headers.Set("X-Frame-Options", "SAMEORIGIN")
		headers.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		c.Next()
	}
}

// corsAllowedHeaders lists the request headers the browser is allowed to send.
var corsAllowedHeaders = strings.Join([]string{
	"Content-Type",
	"Authorization",
	RequestIDHeader,
	"X-Device-Token",
}, ", ")

// corsAllowedMethods lists the methods exposed by the API.
var corsAllowedMethods = strings.Join([]string{
	http.MethodGet,
	http.MethodPost,
	http.MethodPut,
	http.MethodPatch,
	http.MethodDelete,
	http.MethodOptions,
}, ", ")

// CORS implements the origin allow-list.
//
// allowUnknown is only set for local development (empty allow-list) so the Vite
// dev server works out of the box; production always uses an explicit list.
func CORS(allowedOrigins []string, allowUnknown bool) gin.HandlerFunc {
	allowed := make(map[string]struct{}, len(allowedOrigins))
	for _, origin := range allowedOrigins {
		allowed[strings.TrimSpace(origin)] = struct{}{}
	}

	return func(c *gin.Context) {
		origin := strings.TrimSpace(c.GetHeader("Origin"))
		if origin == "" {
			c.Next()
			return
		}

		_, listed := allowed[origin]
		if !listed && !allowUnknown {
			if c.Request.Method == http.MethodOptions {
				c.AbortWithStatus(http.StatusForbidden)
				return
			}
			c.Next()
			return
		}

		headers := c.Writer.Header()
		headers.Set("Access-Control-Allow-Origin", origin)
		headers.Set("Access-Control-Allow-Methods", corsAllowedMethods)
		headers.Set("Access-Control-Allow-Headers", corsAllowedHeaders)
		headers.Set("Access-Control-Expose-Headers", RequestIDHeader)
		headers.Set("Access-Control-Max-Age", "600")
		headers.Add("Vary", "Origin")

		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}
