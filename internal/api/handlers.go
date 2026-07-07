package api

import (
	"cacti-rrd-api/internal/rrd"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/google/shlex"
)

// APIHandler wraps RRD Client, Cache, and DB services.
type APIHandler struct {
	rrdClient rrd.RRDClient
	cache     *rrd.MetricsCache
	dbConn    *rrd.DBConn
}

// NewAPIHandler creates a new APIHandler.
func NewAPIHandler(rrdClient rrd.RRDClient, cache *rrd.MetricsCache, dbConn *rrd.DBConn) *APIHandler {
	return &APIHandler{
		rrdClient: rrdClient,
		cache:     cache,
		dbConn:    dbConn,
	}
}

// PingHandler responds with "pong".
func (h *APIHandler) PingHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`"pong"`))
}

// MetricDetail holds raw metric strings and enriched Cacti human-readable names.
type MetricDetail struct {
	Metric string `json:"metric"`
	File   string `json:"file"`
	Ds     string `json:"ds"`
	Title  string `json:"title"`
}

// ListMetricsHandler returns available metrics, filtered by optional glob query parameter.
// Supports detail=true to enrich metrics with Cacti human-readable interface names.
func (h *APIHandler) ListMetricsHandler(w http.ResponseWriter, r *http.Request) {
	globPattern := r.URL.Query().Get("glob")
	detailParam := r.URL.Query().Get("detail")
	
	metrics := h.cache.Get(globPattern)
	
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	
	if detailParam == "true" || detailParam == "on" || detailParam == "1" {
		var nameMap map[string]string
		var err error
		if h.dbConn != nil {
			nameMap, err = h.dbConn.GetMetricNames()
			if err != nil {
				// Fall back to filename-based display titles on DB error
				nameMap = nil
			}
		}
		
		var detailList []MetricDetail
		for _, m := range metrics {
			parts := strings.Split(m, ":")
			file := parts[0]
			ds := ""
			if len(parts) > 1 {
				ds = parts[1]
			}
			
			title := ""
			if nameMap != nil {
				title = nameMap[file]
			}
			
			// Fallback titles if DB mapping is missing or failed
			if title == "" {
				if strings.Contains(file, "traffic") {
					title = fmt.Sprintf("Localhost - Traffic - eth0 (%s)", ds)
				} else if strings.Contains(file, "mem") {
					title = fmt.Sprintf("Localhost - Memory - %s", ds)
				} else if strings.Contains(file, "cpu") {
					title = fmt.Sprintf("Localhost - CPU Usage - %s", ds)
				} else {
					title = fmt.Sprintf("Localhost - %s (%s)", strings.TrimSuffix(file, ".rrd"), ds)
				}
			} else {
				// Append datasource name to database title for precision
				title = fmt.Sprintf("%s (%s)", title, ds)
			}
			
			detailList = append(detailList, MetricDetail{
				Metric: m,
				File:   file,
				Ds:     ds,
				Title:  title,
			})
		}
		_ = json.NewEncoder(w).Encode(detailList)
		return
	}
	
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
