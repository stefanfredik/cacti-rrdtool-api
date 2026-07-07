package api

import (
	"cacti-rrd-api/internal/config"
	"net/http"
)

// SetupRouter registers API routes with appropriate middleware chains.
func SetupRouter(cfg *config.Config, handler *APIHandler, frontendHandler http.Handler) http.Handler {
	mux := http.NewServeMux()

	// Base middleware stack
	auth := AuthMiddleware(cfg)

	// API routes
	mux.Handle("GET /api/v1/ping", http.HandlerFunc(handler.PingHandler))
	
	mux.Handle("GET /api/v1/list_metrics", auth(http.HandlerFunc(handler.ListMetricsHandler)))
	mux.Handle("POST /api/v1/list_metrics", auth(http.HandlerFunc(handler.ListMetricsHandler)))
	
	mux.Handle("GET /api/v1/xport", auth(http.HandlerFunc(handler.XportHandler)))
	mux.Handle("POST /api/v1/xport", auth(http.HandlerFunc(handler.XportHandler)))
	
	mux.Handle("GET /api/v1/graph", auth(http.HandlerFunc(handler.GraphHandler)))
	mux.Handle("POST /api/v1/graph", auth(http.HandlerFunc(handler.GraphHandler)))

	mux.Handle("GET /api/v1/graphs", auth(http.HandlerFunc(handler.ListGraphsHandler)))
	mux.Handle("GET /api/v1/graphs/render", auth(http.HandlerFunc(handler.RenderGraphByIDHandler)))
	mux.Handle("GET /api/v1/trees", auth(http.HandlerFunc(handler.ListGraphTreesHandler)))

	// Frontend route (serves embedded index.html)
	if frontendHandler != nil {
		mux.Handle("GET /", frontendHandler)
	}

	// Wrap everything with middleware stack: CORS, Rate Limiting, Logger, and RequestID
	var wrapped http.Handler = mux
	wrapped = CORSMiddleware(wrapped)
	wrapped = RateLimitMiddleware(cfg)(wrapped)
	wrapped = LoggerMiddleware(wrapped)
	wrapped = RequestIDMiddleware(wrapped)

	return wrapped
}
