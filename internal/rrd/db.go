package rrd

import (
	"database/sql"
	"fmt"
	"log"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

// DBConn wraps Cacti MariaDB/MySQL connection.
type DBConn struct {
	db *sql.DB
}

// NewDBConn initializes connection to Cacti MySQL database.
// Returns (nil, nil) if database configuration is not provided (falls back to file-only mode).
func NewDBConn(user, pass, host, port, dbname string) (*DBConn, error) {
	if user == "" || host == "" || dbname == "" {
		return nil, nil
	}

	if port == "" {
		port = "3306"
	}

	// Build Data Source Name
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?parseTime=true", user, pass, host, port, dbname)
	
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, err
	}

	// Configure pool parameters
	db.SetMaxOpenConns(5)
	db.SetMaxIdleConns(2)
	db.SetConnMaxLifetime(10 * time.Minute)

	// Validate connection
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("cacti database ping failed: %w", err)
	}

	log.Printf("Successfully connected to Cacti MySQL database at %s:%s/%s", host, port, dbname)
	return &DBConn{db: db}, nil
}

// Close closes the database connection.
func (d *DBConn) Close() {
	if d != nil && d.db != nil {
		_ = d.db.Close()
		log.Println("Cacti database connection closed.")
	}
}

// GetMetricNames maps RRD filenames to their human-readable titles (name_cache) from Cacti database.
func (d *DBConn) GetMetricNames() (map[string]string, error) {
	if d == nil || d.db == nil {
		return nil, nil
	}

	// Query to map RRD filename to the cached human-readable source title
	query := `
		SELECT name_cache, data_source_path 
		FROM data_template_data 
		WHERE data_source_path IS NOT NULL AND data_source_path != ""
	`

	rows, err := d.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to query cacti data templates: %w", err)
	}
	defer rows.Close()

	nameMap := make(map[string]string)
	for rows.Next() {
		var nameCache, sourcePath string
		if err := rows.Scan(&nameCache, &sourcePath); err != nil {
			continue
		}

		// Extract RRD filename from sourcePath
		// e.g. <path_rra>/localhost_traffic_in_4.rrd -> localhost_traffic_in_4.rrd
		parts := strings.Split(sourcePath, "/")
		filename := parts[len(parts)-1]
		if filename != "" {
			nameMap[filename] = nameCache
		}
	}

	return nameMap, nil
}

// GraphDefinition holds the generated specs and title of a Cacti graph.
type GraphDefinition struct {
	ID    int    `json:"id"`
	Title string `json:"title"`
	Specs string `json:"specs"`
}

// GetGraphs queries the Cacti database and dynamically reconstructs the RRDTool graph specs for all graphs.
func (d *DBConn) GetGraphs() ([]GraphDefinition, error) {
	if d == nil || d.db == nil {
		return nil, nil
	}

	// Dynamic query to pull Cacti graph structures
	query := `
		SELECT 
			gl.id AS local_graph_id,
			gtg.title_cache AS graph_title,
			gti.graph_type_id,
			gti.text_format,
			c.hex AS color_hex,
			dtr.data_source_name,
			dtd.data_source_path
		FROM graph_local gl
		INNER JOIN graph_templates_graph gtg ON gl.id = gtg.local_graph_id
		INNER JOIN graph_templates_item gti ON gl.id = gti.local_graph_id
		LEFT JOIN colors c ON gti.color_id = c.id
		LEFT JOIN data_template_rrd dtr ON gti.task_item_id = dtr.id
		LEFT JOIN data_template_data dtd ON dtr.local_data_id = dtd.local_data_id
		WHERE dtd.data_source_path IS NOT NULL AND dtd.data_source_path != ""
		ORDER BY gl.id, gti.sequence
	`

	rows, err := d.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to query Cacti graphs: %w", err)
	}
	defer rows.Close()

	type rawItem struct {
		graphTypeId int
		textFormat  string
		colorHex    string
		dsName      string
		rrdPath     string
	}

	graphsMap := make(map[int]string) // local_graph_id -> title
	itemsMap := make(map[int][]rawItem)

	for rows.Next() {
		var graphID int
		var title string
		var gtype sql.NullInt64
		var text sql.NullString
		var color sql.NullString
		var dsName sql.NullString
		var rrdPath sql.NullString

		err := rows.Scan(&graphID, &title, &gtype, &text, &color, &dsName, &rrdPath)
		if err != nil {
			continue
		}

		graphsMap[graphID] = title

		if dsName.Valid && dsName.String != "" && rrdPath.Valid && rrdPath.String != "" {
			itemsMap[graphID] = append(itemsMap[graphID], rawItem{
				graphTypeId: int(gtype.Int64),
				textFormat:  text.String,
				colorHex:    color.String,
				dsName:      dsName.String,
				rrdPath:     rrdPath.String,
			})
		}
	}

	var results []GraphDefinition

	// Build the rrdtool graph query specs for each graph
	for graphID, title := range graphsMap {
		items := itemsMap[graphID]
		if len(items) == 0 {
			continue
		}

		var defs []string
		var plots []string
		vnameCounter := 0

		// Keep track of registered DEF to avoid duplicates in the same graph command
		registeredDefs := make(map[string]string) // "rrdfile:dsName" -> vname

		for _, item := range items {
			// Extract file name
			parts := strings.Split(item.rrdPath, "/")
			rrdFile := parts[len(parts)-1]
			if rrdFile == "" {
				continue
			}

			key := rrdFile + ":" + item.dsName
			vname, exists := registeredDefs[key]
			if !exists {
				// Generate variable name (e.g. v0, v1, v2...)
				vname = fmt.Sprintf("v%d", vnameCounter)
				vnameCounter++
				registeredDefs[key] = vname
				// Add DEF:vname=rrdfile:ds:AVERAGE
				defs = append(defs, fmt.Sprintf("DEF:%s=%s:%s:AVERAGE", vname, rrdFile, item.dsName))
			}

			// Add LINE or AREA if applicable
			// Cacti graph_type_id: 1=LINE1, 2=LINE2, 3=LINE3, 4=AREA
			var plotType string
			switch item.graphTypeId {
			case 1:
				plotType = "LINE1"
			case 2:
				plotType = "LINE2"
			case 3:
				plotType = "LINE3"
			case 4:
				plotType = "AREA"
			default:
				continue // Skip legends, comments, etc.
			}

			color := item.colorHex
			if color == "" {
				// Fallback to a default color if not specified
				color = "38a169"
			}
			if !strings.HasPrefix(color, "#") {
				color = "#" + color
			}

			legend := item.textFormat
			legend = strings.TrimSpace(legend)
			legend = strings.Trim(legend, `"'`)
			if legend == "" {
				legend = item.dsName
			}

			plots = append(plots, fmt.Sprintf("%s:%s%s:%s", plotType, vname, color, legend))
		}

		if len(defs) == 0 {
			continue
		}

		// Merge DEFs and LINE/AREAs into a single spec query string
		allSpecs := append(defs, plots...)
		specStr := strings.Join(allSpecs, " ")

		results = append(results, GraphDefinition{
			ID:    graphID,
			Title: title,
			Specs: specStr,
		})
	}

	return results, nil
}
