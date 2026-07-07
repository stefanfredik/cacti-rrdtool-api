package rrd

import (
	"bufio"
	"bytes"
	"context"
	"log"
	"strings"
	"sync"
	"time"
)

// MetricsCache manages a list of discovered RRD metrics in memory.
type MetricsCache struct {
	mu              sync.RWMutex
	metrics         []string
	rrdClient       RRDClient
	refreshInterval time.Duration
	stopChan        chan struct{}
}

// NewMetricsCache creates a new MetricsCache.
func NewMetricsCache(rrdClient RRDClient, refreshInterval time.Duration) *MetricsCache {
	if refreshInterval <= 0 {
		refreshInterval = 5 * time.Minute
	}
	return &MetricsCache{
		rrdClient:       rrdClient,
		refreshInterval: refreshInterval,
		stopChan:        make(chan struct{}),
	}
}

// Start launches the background cache refresher.
func (c *MetricsCache) Start(ctx context.Context) {
	// Initial population
	go func() {
		log.Println("Initializing metrics cache...")
		c.refresh()
		log.Printf("Metrics cache initialized with %d metrics.", c.Size())

		ticker := time.NewTicker(c.refreshInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				log.Println("Refreshing metrics cache...")
				c.refresh()
				log.Printf("Metrics cache refreshed: %d metrics.", c.Size())
			case <-c.stopChan:
				return
			case <-ctx.Done():
				return
			}
		}
	}()
}

// Stop stops the background refresher.
func (c *MetricsCache) Stop() {
	close(c.stopChan)
}

// Size returns the number of metrics currently in the cache.
func (c *MetricsCache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.metrics)
}

// Get returns the list of cached metrics, optionally filtered by a glob pattern.
func (c *MetricsCache) Get(globPattern string) []string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if globPattern == "" || globPattern == "*" {
		// Return copy of all metrics
		res := make([]string, len(c.metrics))
		copy(res, c.metrics)
		return res
	}

	var matched []string
	for _, m := range c.metrics {
		if globMatch(globPattern, m) {
			matched = append(matched, m)
		}
	}
	return matched
}

// refresh performs the directory walk and metadata extraction.
func (c *MetricsCache) refresh() {
	ctx := context.Background()
	rrdFiles, err := c.rrdClient.ListRRDs(ctx)
	if err != nil {
		log.Printf("Error listing RRD files during refresh: %s", err)
		return
	}

	var newMetrics []string
	var wg sync.WaitGroup
	var mu sync.Mutex

	// Limit concurrency to avoid overloading system resources
	sem := make(chan struct{}, 8)

	for _, file := range rrdFiles {
		wg.Add(1)
		go func(f string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			infoBytes, err := c.rrdClient.Info(ctx, f)
			if err != nil {
				log.Printf("Error getting info for RRD %s: %s", f, err)
				return
			}

			dsNames := parseDataSources(infoBytes)
			
			mu.Lock()
			for _, ds := range dsNames {
				// Format is file_relative_path:ds_name
				// e.g. localhost_mem_buffers_3.rrd:mem_buffers
				newMetrics = append(newMetrics, f+":"+ds)
			}
			mu.Unlock()
		}(file)
	}

	wg.Wait()

	c.mu.Lock()
	c.metrics = newMetrics
	c.mu.Unlock()
}

// parseDataSources parses the data sources (DS) from rrdtool info output.
func parseDataSources(infoBytes []byte) []string {
	var dsNames []string
	dsMap := make(map[string]bool)

	scanner := bufio.NewScanner(bytes.NewReader(infoBytes))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "ds[") {
			idx := strings.IndexByte(line, ']')
			if idx > 3 {
				dsName := line[3:idx]
				if !dsMap[dsName] {
					dsMap[dsName] = true
					dsNames = append(dsNames, dsName)
				}
			}
		}
	}
	return dsNames
}

// globMatch checks if name matches the pattern. Supports '*' wildcard.
func globMatch(pattern, name string) bool {
	// A simple but highly effective glob implementation for metrics filtering.
	// E.g., "localhost_*" matches "localhost_mem_buffers_3.rrd:mem_buffers".
	parts := strings.Split(pattern, "*")
	if len(parts) == 1 {
		return pattern == name
	}

	if !strings.HasPrefix(name, parts[0]) {
		return false
	}

	// Simple check for wildcard at end: "foo*"
	if len(parts) == 2 && parts[1] == "" {
		return true
	}

	// Match middle parts
	nameIdx := len(parts[0])
	for i := 1; i < len(parts)-1; i++ {
		part := parts[i]
		if part == "" {
			continue
		}
		idx := strings.Index(name[nameIdx:], part)
		if idx == -1 {
			return false
		}
		nameIdx += idx + len(part)
	}

	// Match final part
	lastPart := parts[len(parts)-1]
	if lastPart != "" {
		return strings.HasSuffix(name, lastPart)
	}

	return true
}
