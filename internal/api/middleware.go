package api

import (
	"bytes"
	"cacti-rrd-api/internal/config"
	"cacti-rrd-api/internal/querysign"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"
)

type contextKey string

const requestIDKey contextKey = "request_id"

// generateRequestID generates a unique 16-byte random request ID.
func generateRequestID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// GetRequestID extracts the request ID from context.
func GetRequestID(ctx context.Context) string {
	if id, ok := ctx.Value(requestIDKey).(string); ok {
		return id
	}
	return ""
}

// RequestIDMiddleware injects a unique request ID into context and response headers.
func RequestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqID := r.Header.Get("X-Request-ID")
		if reqID == "" {
			reqID = generateRequestID()
		}
		
		w.Header().Set("X-Request-ID", reqID)
		ctx := context.WithValue(r.Context(), requestIDKey, reqID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// CORSMiddleware adds CORS headers to the response.
func CORSMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS, PUT, DELETE")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Requested-With, X-Request-ID")
		w.Header().Set("Access-Control-Allow-Credentials", "true")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// LogEntry defines structured log schema for networking.
type LogEntry struct {
	Timestamp string `json:"timestamp"`
	RequestID string `json:"request_id"`
	Method    string `json:"method"`
	Path      string `json:"path"`
	Status    int    `json:"status"`
	Duration  string `json:"duration"`
	ClientIP  string `json:"client_ip"`
}

// LoggerMiddleware logs HTTP request details in structured JSON format.
func LoggerMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		reqID := GetRequestID(r.Context())
		
		// Capture client IP correctly (support reverse proxy headers)
		clientIP := r.Header.Get("X-Forwarded-For")
		if clientIP == "" {
			clientIP = r.RemoteAddr
		}
		
		wrappedWriter := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
		next.ServeHTTP(wrappedWriter, r)
		
		entry := LogEntry{
			Timestamp: time.Now().UTC().Format(time.RFC3339),
			RequestID: reqID,
			Method:    r.Method,
			Path:      r.URL.Path,
			Status:    wrappedWriter.statusCode,
			Duration:  time.Since(start).String(),
			ClientIP:  clientIP,
		}
		
		logBytes, err := json.Marshal(entry)
		if err == nil {
			log.Println(string(logBytes))
		} else {
			log.Printf("method=%s path=%s status=%d", r.Method, r.URL.Path, wrappedWriter.statusCode)
		}
	})
}

type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// TokenBucket implements a thread-safe token-bucket rate limiter.
type TokenBucket struct {
	mu           sync.Mutex
	tokens       float64
	maxTokens    float64
	replenishRPS float64
	lastChecked  time.Time
}

func NewTokenBucket(rps float64, burst int) *TokenBucket {
	return &TokenBucket{
		tokens:       float64(burst),
		maxTokens:    float64(burst),
		replenishRPS: rps,
		lastChecked:  time.Now(),
	}
}

func (tb *TokenBucket) Allow() bool {
	tb.mu.Lock()
	defer tb.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(tb.lastChecked).Seconds()
	tb.lastChecked = now

	tb.tokens += elapsed * tb.replenishRPS
	if tb.tokens > tb.maxTokens {
		tb.tokens = tb.maxTokens
	}

	if tb.tokens >= 1.0 {
		tb.tokens -= 1.0
		return true
	}
	return false
}

// RateLimitMiddleware enforces rate limiting based on client IP or global bucket.
func RateLimitMiddleware(cfg *config.Config) func(http.Handler) http.Handler {
	var tb *TokenBucket
	if cfg.RateLimitRPS > 0 {
		burst := cfg.RateLimitBurst
		if burst <= 0 {
			burst = int(cfg.RateLimitRPS)
		}
		tb = NewTokenBucket(cfg.RateLimitRPS, burst)
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if tb != nil {
				if !tb.Allow() {
					w.Header().Set("Content-Type", "application/json; charset=utf-8")
					w.WriteHeader(http.StatusTooManyRequests)
					_, _ = w.Write([]byte(`{"error":"Too Many Requests","message":"Rate limit exceeded"}`))
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// AuthMiddleware enforces HMAC signature verification, Basic Auth, or both depending on config.
func AuthMiddleware(cfg *config.Config) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var queryParams url.Values
			var rawData []byte
			var err error

			// 1. Read Request Data
			if r.Method == http.MethodPost {
				rawData, err = io.ReadAll(r.Body)
				if err != nil {
					http.Error(w, "unable to read request body", http.StatusBadRequest)
					return
				}
				// Restore body
				r.Body = io.NopCloser(bytes.NewBuffer(rawData))
				queryParams, _ = url.ParseQuery(string(rawData))
			} else {
				queryParams = r.URL.Query()
				rawData = []byte(r.URL.RawQuery)
			}

			// 2. Check Expiry
			xVal := queryParams.Get("x")
			if xVal != "" {
				expiry, err := strconv.ParseInt(xVal, 10, 64)
				if err != nil || expiry < time.Now().Unix() {
					http.Error(w, "request has expired", http.StatusUnauthorized)
					return
				}
			}

			// 3. HMAC Signature Check
			sVal := queryParams.Get("s")
			if sVal != "" && len(cfg.SignedQuerySecret) > 0 {
				path := []byte(r.URL.Path)
				if querysign.ValidateSignedQuery([]byte(cfg.SignedQuerySecret), path, rawData) {
					// HMAC is valid, bypass other auth
					next.ServeHTTP(w, r)
					return
				}
				http.Error(w, "signature failure", http.StatusUnauthorized)
				return
			}

			// 4. Basic Auth Check
			if cfg.BasicAuthUser != "" && cfg.BasicAuthPass != "" {
				user, pass, ok := r.BasicAuth()
				if ok && user == cfg.BasicAuthUser && pass == cfg.BasicAuthPass {
					next.ServeHTTP(w, r)
					return
				}
				w.Header().Set("WWW-Authenticate", `Basic realm="Cacti RRD API"`)
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}

			// 5. Allow if no authentication is configured
			if len(cfg.SignedQuerySecret) == 0 && cfg.BasicAuthUser == "" {
				next.ServeHTTP(w, r)
				return
			}

			// Auth required but missing/invalid
			http.Error(w, "forbidden", http.StatusForbidden)
		})
	}
}
