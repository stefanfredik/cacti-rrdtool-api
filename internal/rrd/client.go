package rrd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// RRDClient defines the contract for interacting with RRD databases.
type RRDClient interface {
	Xport(ctx context.Context, format, start, end, step, maxrows string, specs []string) ([]byte, error)
	Graph(ctx context.Context, start, end, step, imgFormat string, options map[string]string, specs []string) ([]byte, error)
	Info(ctx context.Context, relativePath string) ([]byte, error)
	ListRRDs(ctx context.Context) ([]string, error)
}

// CLIClient implements RRDClient by calling the rrdtool CLI.
type CLIClient struct {
	rrdtoolBin string
	rrdDir     string
	timeout    time.Duration
	sem        chan struct{}
}

// NewCLIClient creates a new CLIClient with a process execution concurrency limit.
func NewCLIClient(rrdtoolBin, rrdDir string, timeout time.Duration, maxConns int) *CLIClient {
	if maxConns <= 0 {
		maxConns = 10 // Sensible default limit
	}
	return &CLIClient{
		rrdtoolBin: rrdtoolBin,
		rrdDir:     rrdDir,
		timeout:    timeout,
		sem:        make(chan struct{}, maxConns),
	}
}

// SanitizeAndRewritePath validates that the rrd file is inside rrdDir and rewrites it to an absolute path.
func (c *CLIClient) SanitizeAndRewritePath(rrdPath string) (string, error) {
	// Clean the path to resolve any ".."
	cleaned := filepath.Clean(rrdPath)

	// Ensure the path is relative and does not escape the directory
	if filepath.IsAbs(cleaned) || strings.HasPrefix(cleaned, "..") {
		return "", fmt.Errorf("invalid RRD path: path traversal detected or absolute path not allowed")
	}

	// Make sure it has .rrd extension
	if !strings.HasSuffix(strings.ToLower(cleaned), ".rrd") {
		return "", fmt.Errorf("invalid RRD file: must end in .rrd")
	}

	// Return absolute path
	return filepath.Join(c.rrdDir, cleaned), nil
}

// RewriteSpecs parses the DEF arguments and rewrites relative RRD paths to secure absolute paths.
func (c *CLIClient) RewriteSpecs(specs []string) ([]string, error) {
	rewritten := make([]string, len(specs))
	for i, spec := range specs {
		if strings.HasPrefix(spec, "DEF:") {
			// Format is DEF:<vname>=<rrdfile>:<ds-name>:<CF>...
			parts := strings.Split(spec, ":")
			if len(parts) >= 4 {
				eqParts := strings.SplitN(parts[1], "=", 2)
				if len(eqParts) == 2 {
					rrdFile := eqParts[1]
					absPath, err := c.SanitizeAndRewritePath(rrdFile)
					if err != nil {
						return nil, err
					}
					eqParts[1] = absPath
					parts[1] = strings.Join(eqParts, "=")
					rewritten[i] = strings.Join(parts, ":")
					continue
				}
			}
		}
		rewritten[i] = spec
	}
	return rewritten, nil
}

// Xport executes the "rrdtool xport" command.
func (c *CLIClient) Xport(ctx context.Context, format, start, end, step, maxrows string, specs []string) ([]byte, error) {
	rewrittenSpecs, err := c.RewriteSpecs(specs)
	if err != nil {
		return nil, err
	}

	args := []string{"xport"}
	if format == "" || strings.ToLower(format) == "json" {
		args = append(args, "--json")
	}
	if start != "" {
		args = append(args, "--start", start)
	}
	if end != "" {
		args = append(args, "--end", end)
	}
	if step != "" {
		args = append(args, "--step", step)
	}
	if maxrows != "" {
		args = append(args, "--maxrows", maxrows)
	}
	args = append(args, "--")
	args = append(args, rewrittenSpecs...)

	return c.runCmd(ctx, args)
}

// Graph executes the "rrdtool graph" command.
func (c *CLIClient) Graph(ctx context.Context, start, end, step, imgFormat string, options map[string]string, specs []string) ([]byte, error) {
	rewrittenSpecs, err := c.RewriteSpecs(specs)
	if err != nil {
		return nil, err
	}

	if imgFormat == "" {
		imgFormat = "SVG"
	}
	imgFormat = strings.ToUpper(imgFormat)

	args := []string{"graph", "-", "--imgformat", imgFormat}
	if start != "" {
		args = append(args, "--start", start)
	}
	if end != "" {
		args = append(args, "--end", end)
	}
	if step != "" {
		args = append(args, "--step", step)
	}

	for k, v := range options {
		if v == "on" {
			args = append(args, "--"+k)
		} else if v != "" {
			args = append(args, "--"+k, v)
		}
	}

	args = append(args, "--")
	args = append(args, rewrittenSpecs...)

	return c.runCmd(ctx, args)
}

// Info executes the "rrdtool info" command.
func (c *CLIClient) Info(ctx context.Context, relativePath string) ([]byte, error) {
	absPath, err := c.SanitizeAndRewritePath(relativePath)
	if err != nil {
		return nil, err
	}
	return c.runCmd(ctx, []string{"info", absPath})
}

// ListRRDs walks the rrdDir and finds all .rrd files (returns relative paths).
func (c *CLIClient) ListRRDs(ctx context.Context) ([]string, error) {
	var files []string
	err := filepath.WalkDir(c.rrdDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(strings.ToLower(d.Name()), ".rrd") {
			rel, err := filepath.Rel(c.rrdDir, path)
			if err == nil {
				files = append(files, rel)
			}
		}
		return nil
	})
	return files, err
}

func (c *CLIClient) runCmd(ctx context.Context, args []string) ([]byte, error) {
	// Acquire semaphore token
	select {
	case c.sem <- struct{}{}:
		defer func() { <-c.sem }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	cmd := exec.CommandContext(timeoutCtx, c.rrdtoolBin, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if stderr.Len() > 0 {
			return nil, errors.New(stderr.String())
		}
		return nil, err
	}
	return stdout.Bytes(), nil
}

// MockClient simulates rrdtool for demonstration or testing.
type MockClient struct {
	rrdDir string
}

func NewMockClient(rrdDir string) *MockClient {
	return &MockClient{rrdDir: rrdDir}
}

func (m *MockClient) Xport(ctx context.Context, format, start, end, step, maxrows string, specs []string) ([]byte, error) {
	// Parse times
	startTime := time.Now().Add(-24 * time.Hour)
	endTime := time.Now()
	stepSec := int64(300)

	if start != "" {
		if t, err := parseRelativeTime(start); err == nil {
			startTime = t
		}
	}
	if end != "" {
		if t, err := parseRelativeTime(end); err == nil {
			endTime = t
		}
	}

	// map from vname (e.g. "a") to ds-name (e.g. "traffic_in")
	vnameToDs := make(map[string]string)
	// map from vname to file-name (e.g. "traffic.rrd")
	vnameToFile := make(map[string]string)

	for _, spec := range specs {
		if strings.HasPrefix(spec, "DEF:") {
			parts := strings.Split(spec, ":")
			if len(parts) >= 4 {
				eqParts := strings.SplitN(parts[1], "=", 2)
				if len(eqParts) == 2 {
					vname := eqParts[0]
					rrdfile := eqParts[1]
					dsName := parts[2]
					vnameToDs[vname] = dsName
					vnameToFile[vname] = rrdfile
				}
			}
		}
	}

	type exportCol struct {
		vname  string
		legend string
		dsName string
		file   string
	}
	var cols []exportCol

	for _, spec := range specs {
		if strings.HasPrefix(spec, "XPORT:") {
			parts := strings.SplitN(spec, ":", 3)
			if len(parts) >= 2 {
				vname := parts[1]
				legend := ""
				if len(parts) == 3 {
					legend = parts[2]
				} else {
					legend = vname
				}
				// remove optional quotes from legend
				legend = strings.Trim(legend, `"'`)
				
				cols = append(cols, exportCol{
					vname:  vname,
					legend: legend,
					dsName: vnameToDs[vname],
					file:   vnameToFile[vname],
				})
			}
		}
	}

	// Fallback if no XPORT statements are found
	if len(cols) == 0 {
		for vname, dsName := range vnameToDs {
			cols = append(cols, exportCol{
				vname:  vname,
				legend: dsName,
				dsName: dsName,
				file:   vnameToFile[vname],
			})
		}
	}

	// If still empty, fall back to "value"
	if len(cols) == 0 {
		cols = append(cols, exportCol{
			vname:  "val",
			legend: "value",
			dsName: "value",
			file:   "unknown.rrd",
		})
	}

	startUnix := startTime.Unix()
	endUnix := endTime.Unix()

	var legend []string
	for _, col := range cols {
		legend = append(legend, col.legend)
	}

	var dataRows []string
	count := 0
	for t := startUnix; t <= endUnix; t += stepSec {
		var vals []string
		for colIdx, col := range cols {
			amplitude := 30.0
			offset := 50.0
			phase := float64(colIdx) * (math.Pi / 4.0)

			if strings.Contains(col.dsName, "out") || strings.Contains(col.legend, "out") {
				amplitude = 20.0
				offset = 35.0
			} else if strings.Contains(col.dsName, "user") {
				offset = 20.0
				amplitude = 15.0
			} else if strings.Contains(col.dsName, "system") {
				offset = 10.0
				amplitude = 5.0
			}

			val := offset + amplitude*math.Sin(float64(t)/3600.0+phase) + 5.0*math.Sin(float64(t)/300.0)
			if val < 0 {
				val = 0
			}
			val = math.Round(val*10000) / 10000
			vals = append(vals, fmt.Sprintf("%.4f", val))
		}

		dataRows = append(dataRows, fmt.Sprintf("[%d,[%s]]", t, strings.Join(vals, ",")))
		count++
		if count > 5000 {
			break
		}
	}

	jsonStr := fmt.Sprintf(`{
  "meta": {
    "start": %d,
    "step": %d,
    "end": %d,
    "legend": [%s]
  },
  "data": [%s]
}`, startUnix, stepSec, endUnix, `"`+strings.Join(legend, `","`)+`"`, strings.Join(dataRows, ","))

	return []byte(jsonStr), nil
}

func (m *MockClient) Graph(ctx context.Context, start, end, step, imgFormat string, options map[string]string, specs []string) ([]byte, error) {
	// Return a beautiful SVG graph
	width := 600
	height := 300
	
	startTime := time.Now().Add(-24 * time.Hour)
	endTime := time.Now()
	if start != "" {
		if t, err := parseRelativeTime(start); err == nil {
			startTime = t
		}
	}
	if end != "" {
		if t, err := parseRelativeTime(end); err == nil {
			endTime = t
		}
	}

	title := "Cacti System Metrics (Demo Mode)"
	if t, ok := options["title"]; ok && t != "" {
		title = t
	}

	// map from vname to dsName
	vnameToDs := make(map[string]string)
	for _, spec := range specs {
		if strings.HasPrefix(spec, "DEF:") {
			parts := strings.Split(spec, ":")
			if len(parts) >= 4 {
				eqParts := strings.SplitN(parts[1], "=", 2)
				if len(eqParts) == 2 {
					vnameToDs[eqParts[0]] = parts[2]
				}
			}
		}
	}

	type graphElement struct {
		isArea bool
		vname  string
		color  string
		legend string
	}
	var elements []graphElement

	for _, spec := range specs {
		isArea := strings.HasPrefix(spec, "AREA:")
		isLine := strings.HasPrefix(spec, "LINE")
		if isArea || isLine {
			parts := strings.SplitN(spec, ":", 3)
			if len(parts) >= 2 {
				vnameColor := parts[1]
				vname := vnameColor
				color := "#38a169"
				
				hashIdx := strings.IndexByte(vnameColor, '#')
				if hashIdx != -1 {
					vname = vnameColor[:hashIdx]
					color = vnameColor[hashIdx:]
				}
				
				legend := ""
				if len(parts) == 3 {
					legend = parts[2]
				} else {
					legend = vname
				}
				legend = strings.Trim(legend, `"'`)
				
				elements = append(elements, graphElement{
					isArea: isArea,
					vname:  vname,
					color:  color,
					legend: legend,
				})
			}
		}
	}

	// Default fallback if no LINE or AREA is specified
	if len(elements) == 0 {
		for vname, dsName := range vnameToDs {
			elements = append(elements, graphElement{
				isArea: true,
				vname:  vname,
				color:  "#38a169",
				legend: dsName,
			})
			break
		}
	}

	// If still empty
	if len(elements) == 0 {
		elements = append(elements, graphElement{
			isArea: true,
			vname:  "val",
			color:  "#38a169",
			legend: "metric",
		})
	}

	pointsCount := 50
	paddingLeft := 60
	paddingRight := 30
	paddingTop := 50
	paddingBottom := 60
	graphWidth := width - paddingLeft - paddingRight
	graphHeight := height - paddingTop - paddingBottom

	var paths []string
	var legendsSVG []string

	for elIdx, el := range elements {
		pathPoints := ""
		var values []float64
		
		phase := float64(elIdx) * (math.Pi / 4.0)
		amplitude := 30.0 - float64(elIdx)*5.0
		offset := 50.0 - float64(elIdx)*10.0
		if amplitude < 5 { amplitude = 5 }
		if offset < 10 { offset = 10 }
		
		for i := 0; i < pointsCount; i++ {
			t := float64(i)
			val := offset + amplitude*math.Sin(t/5.0+phase) + 5.0*math.Cos(t/2.0)
			if val < 0 { val = 0 }
			if val > 100 { val = 100 }
			values = append(values, val)
		}
		
		for i, val := range values {
			x := paddingLeft + (i * graphWidth / (pointsCount - 1))
			y := paddingTop + graphHeight - int(val*float64(graphHeight)/100.0)
			if i == 0 {
				pathPoints += fmt.Sprintf("M %d %d", x, y)
			} else {
				pathPoints += fmt.Sprintf(" L %d %d", x, y)
			}
		}
		
		color := el.color
		if !strings.HasPrefix(color, "#") {
			color = "#" + color
		}
		
		if el.isArea {
			areaPath := fmt.Sprintf("%s L %d %d L %d %d Z", pathPoints, paddingLeft+graphWidth, paddingTop+graphHeight, paddingLeft, paddingTop+graphHeight)
			paths = append(paths, fmt.Sprintf(`<path d="%s" fill="%s" opacity="0.15" />`, areaPath, color))
		}
		paths = append(paths, fmt.Sprintf(`<path d="%s" fill="none" stroke="%s" stroke-width="2" />`, pathPoints, color))
		
		legY := paddingTop + graphHeight + 30 + (elIdx/2)*15
		legX := paddingLeft + (elIdx%2)*250
		legendsSVG = append(legendsSVG, fmt.Sprintf(`
			<rect x="%d" y="%d" width="10" height="10" fill="%s" rx="2" />
			<text x="%d" y="%d" fill="#e2e8f0" font-size="10" font-weight="500">%s</text>
		`, legX, legY-8, color, legX+15, legY, el.legend))
	}

	svg := fmt.Sprintf(`<svg width="%d" height="%d" viewBox="0 0 %d %d" xmlns="http://www.w3.org/2000/svg" style="background:#111217; font-family:Inter, Roboto, sans-serif;">
	<defs>
	</defs>
	<!-- Title -->
	<text x="%d" y="25" fill="#f7fafc" font-size="14" font-weight="bold" text-anchor="middle">%s</text>
	<text x="%d" y="40" fill="#a0aec0" font-size="9" text-anchor="middle">Range: %s to %s</text>

	<!-- Grid Lines -->
	<line x1="%d" y1="%d" x2="%d" y2="%d" stroke="#2d3748" stroke-width="1"/>
	<line x1="%d" y1="%d" x2="%d" y2="%d" stroke="#2d3748" stroke-width="1"/>
	<line x1="%d" y1="%d" x2="%d" y2="%d" stroke="#2d3748" stroke-width="1"/>
	<line x1="%d" y1="%d" x2="%d" y2="%d" stroke="#2d3748" stroke-dasharray="3,3" stroke-width="1"/>

	<!-- Y-Axis Labels -->
	<text x="%d" y="%d" fill="#718096" font-size="9" text-anchor="end">100</text>
	<text x="%d" y="%d" fill="#718096" font-size="9" text-anchor="end">50</text>
	<text x="%d" y="%d" fill="#718096" font-size="9" text-anchor="end">0</text>

	<!-- X-Axis Labels -->
	<text x="%d" y="%d" fill="#718096" font-size="9" text-anchor="start">%s</text>
	<text x="%d" y="%d" fill="#718096" font-size="9" text-anchor="end">%s</text>

	<!-- Chart Area & Paths -->
	%s

	<!-- Legend -->
	%s
</svg>`,
		width, height, width, height,
		width/2, title,
		width/2, startTime.Format("2006-01-02 15:04:05"), endTime.Format("2006-01-02 15:04:05"),
		// Grid lines
		paddingLeft, paddingTop, width-paddingRight, paddingTop,
		paddingLeft, paddingTop + graphHeight/2, width-paddingRight, paddingTop + graphHeight/2,
		paddingLeft, paddingTop + graphHeight, width-paddingRight, paddingTop + graphHeight,
		// Vertical axis line
		paddingLeft, paddingTop, paddingLeft, paddingTop + graphHeight,
		// Y Axis labels
		paddingLeft - 8, paddingTop + 4,
		paddingLeft - 8, paddingTop + graphHeight/2 + 4,
		paddingLeft - 8, paddingTop + graphHeight + 4,
		// X Axis labels
		paddingLeft, paddingTop + graphHeight + 15, startTime.Format("15:04"),
		width - paddingRight, paddingTop + graphHeight + 15, endTime.Format("15:04"),
		// Chart Area & Paths
		strings.Join(paths, "\n"),
		// Legend
		strings.Join(legendsSVG, "\n"),
	)

	return []byte(svg), nil
}

func (m *MockClient) Info(ctx context.Context, relativePath string) ([]byte, error) {
	// Simulate output of rrdtool info
	cleaned := filepath.Base(relativePath)
	dsName := "mem_buffers"
	if strings.Contains(cleaned, "cpu") {
		dsName = "cpu_system"
	} else if strings.Contains(cleaned, "traffic") {
		dsName = "traffic_in"
	}

	infoStr := fmt.Sprintf(`filename = "%s"
rrd_version = "0003"
step = 300
last_update = 1751912400
header_size = 2872
ds[%s].type = "GAUGE"
ds[%s].minimal_heartbeat = 600
ds[%s].min = 0.0000000000e+00
ds[%s].max = NaN
ds[%s].last_ds = "UNKN"
ds[%s].value = 0.0000000000e+00
ds[%s].unknown_sec = 0
`, relativePath, dsName, dsName, dsName, dsName, dsName, dsName, dsName)

	if dsName == "traffic_in" {
		infoStr += `ds[traffic_out].type = "GAUGE"
ds[traffic_out].minimal_heartbeat = 600
ds[traffic_out].min = 0.0000000000e+00
ds[traffic_out].max = NaN
`
	}

	return []byte(infoStr), nil
}

func (m *MockClient) ListRRDs(ctx context.Context) ([]string, error) {
	// Return some mock RRD files
	return []string{
		"localhost_mem_buffers_3.rrd",
		"localhost_cpu_system_1.rrd",
		"localhost_cpu_user_2.rrd",
		"localhost_traffic_in_4.rrd",
	}, nil
}

func parseRelativeTime(s string) (time.Time, error) {
	if s == "now" {
		return time.Now(), nil
	}
	if strings.HasPrefix(s, "-") {
		// e.g. -1h, -24h, -1d
		durationStr := s[1:]
		if strings.HasSuffix(durationStr, "d") {
			daysStr := durationStr[:len(durationStr)-1]
			days, err := strconv.Atoi(daysStr)
			if err != nil {
				return time.Time{}, err
			}
			return time.Now().AddDate(0, 0, -days), nil
		}
		d, err := time.ParseDuration(durationStr)
		if err != nil {
			return time.Time{}, err
		}
		return time.Now().Add(-d), nil
	}
	// Parse as absolute epoch
	sec, err := strconv.ParseInt(s, 10, 64)
	if err == nil {
		return time.Unix(sec, 0), nil
	}
	return time.Time{}, fmt.Errorf("invalid time format")
}
