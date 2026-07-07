package api

import (
	"cacti-rrd-api/internal/rrd"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/google/shlex"
)

// APIHandler wraps RRD Client and Cache services.
type APIHandler struct {
	rrdClient rrd.RRDClient
	cache     *rrd.MetricsCache
}

// NewAPIHandler creates a new APIHandler.
func NewAPIHandler(rrdClient rrd.RRDClient, cache *rrd.MetricsCache) *APIHandler {
	return &APIHandler{
		rrdClient: rrdClient,
		cache:     cache,
	}
}

// PingHandler responds with "pong".
func (h *APIHandler) PingHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`"pong"`))
}

// ListMetricsHandler returns available metrics, filtered by optional glob query parameter.
func (h *APIHandler) ListMetricsHandler(w http.ResponseWriter, r *http.Request) {
	globPattern := r.URL.Query().Get("glob")
	
	metrics := h.cache.Get(globPattern)
	
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(metrics)
}

// XportHandler runs rrdtool xport.
func (h *APIHandler) XportHandler(w http.ResponseWriter, r *http.Request) {
	queryParams := r.URL.Query()
	
	start := queryParams.Get("start")
	end := queryParams.Get("end")
	step := queryParams.Get("step")
	maxrows := queryParams.Get("maxrows")
	format := queryParams.Get("format")
	xportSpec := queryParams.Get("xport")

	if xportSpec == "" {
		http.Error(w, "empty xport specification", http.StatusBadRequest)
		return
	}

	specs, err := shlex.Split(xportSpec)
	if err != nil {
		http.Error(w, fmt.Sprintf("unable to perform arg splitting on xport specification: %s", err), http.StatusBadRequest)
		return
	}

	out, err := h.rrdClient.Xport(r.Context(), format, start, end, step, maxrows, specs)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if format == "" || strings.ToLower(format) == "json" {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
	} else {
		w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	}
	
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(out)
}

var graphFlagToWantArg = map[string]bool{
	"start":                    true,
	"step":                     true,
	"end":                      true,
	"title":                    true,
	"vertical-label":           true,
	"width":                    true,
	"height":                   true,
	"upper-limit":              true,
	"lower-limit":              true,
	"x-grid":                   true,
	"week-fmt":                 true,
	"y-grid":                   true,
	"left-axis-formatter":      true,
	"left-axis-format":         true,
	"units-exponent":           true,
	"units-length":             true,
	"units":                    true,
	"right-axis":               true,
	"right-axis-label":         true,
	"right-axis-formatter":     true,
	"right-axis-format":        true,
	"legend-position":          true,
	"legend-direction":         true,
	"daemon":                   true,
	"imginfo":                  true,
	"color":                    true,
	"grid-dash":                true,
	"border":                   true,
	"zoom":                     true,
	"font":                     true,
	"font-render-mode":         true,
	"font-smoothing-threshold": true,
	"graph-render-mode":        true,
	"imgformat":                true,
	"tabwidth":                 true,
	"base":                     true,
	"watermark":                true,

	"only-graph":                   false,
	"full-size-mode":               false,
	"rigid":                        false,
	"allow-shrink":                 false,
	"alt-autoscale":                false,
	"alt-autoscale-min":            false,
	"alt-autoscale-max":            false,
	"no-gridfit":                   false,
	"alt-y-grid":                   false,
	"logarithmic":                  false,
	"no-legend":                    false,
	"force-rules-legend":           false,
	"lazy":                         false,
	"dynamic-labels":               false,
	"pango-markup":                 false,
	"slope-mode":                   false,
	"interlaced":                   false,
	"use-nan-for-all-missing-data": false,
}

var imgFormatToContentType = map[string]string{
	"PNG": "image/png",
	"SVG": "image/svg+xml",
}

// GraphHandler runs rrdtool graph.
func (h *APIHandler) GraphHandler(w http.ResponseWriter, r *http.Request) {
	queryParams := r.URL.Query()
	
	start := queryParams.Get("start")
	end := queryParams.Get("end")
	step := queryParams.Get("step")
	imgFormat := queryParams.Get("imgformat")
	graphSpec := queryParams.Get("graph")

	if graphSpec == "" {
		http.Error(w, "empty graph specification", http.StatusBadRequest)
		return
	}

	if imgFormat == "" {
		imgFormat = "SVG"
	}
	imgFormat = strings.ToUpper(imgFormat)
	
	contentType, formatSupported := imgFormatToContentType[imgFormat]
	if !formatSupported {
		http.Error(w, fmt.Sprintf("graph api does not support the %q format", imgFormat), http.StatusBadRequest)
		return
	}

	options := make(map[string]string)
	for k, vs := range queryParams {
		if len(vs) == 0 {
			continue
		}
		v := vs[0]
		
		// Skip standard params already handled
		if k == "graph" || k == "imgformat" || k == "start" || k == "end" || k == "step" || k == "s" || k == "x" {
			continue
		}

		wantArg, ok := graphFlagToWantArg[k]
		if !ok {
			// Skip unknown options
			continue
		}

		if wantArg {
			options[k] = v
		} else {
			if v == "on" || v == "true" {
				options[k] = "on"
			}
		}
	}

	specs, err := shlex.Split(graphSpec)
	if err != nil {
		http.Error(w, fmt.Sprintf("unable to perform arg splitting on graph specification: %s", err), http.StatusBadRequest)
		return
	}

	out, err := h.rrdClient.Graph(r.Context(), start, end, step, imgFormat, options, specs)
	if err != nil {
		// Log errors since rrdtool graph stderr details are helpful
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(out)
}

// RestrictToMethods rejects requests with unallowed HTTP methods.
func RestrictToMethods(methods ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			allowed := false
			for _, m := range methods {
				if r.Method == m {
					allowed = true
					break
				}
			}
			if !allowed {
				w.Header().Set("Allow", strings.Join(methods, ", "))
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
