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

// TreeItem represents a node in Cacti Graph Tree.
type TreeItem struct {
	ID           int        `json:"id"`
	ParentID     int        `json:"parent_id"`
	Type         string     `json:"type"` // "header", "host", "graph"
	Title        string     `json:"title"`
	HostID       int        `json:"host_id,omitempty"`
	LocalGraphID int        `json:"local_graph_id,omitempty"`
	Children     []TreeItem `json:"children"`
}

// GraphTree represents a root Cacti Graph Tree.
type GraphTree struct {
	ID    int        `json:"id"`
	Name  string     `json:"name"`
	Items []TreeItem `json:"items"`
}

// GetGraphTrees retrieves the hierarchical Graph Trees from Cacti database.
func (d *DBConn) GetGraphTrees() ([]GraphTree, error) {
	if d == nil || d.db == nil {
		return nil, nil
	}

	// 1. Fetch all Trees
	treeQuery := "SELECT id, name FROM graph_tree ORDER BY name"
	treeRows, err := d.db.Query(treeQuery)
	if err != nil {
		return nil, fmt.Errorf("failed to query graph trees: %w", err)
	}
	defer treeRows.Close()

	var trees []GraphTree
	for treeRows.Next() {
		var gt GraphTree
		if err := treeRows.Scan(&gt.ID, &gt.Name); err == nil {
			trees = append(trees, gt)
		}
	}

	// 2. For each tree, build hierarchical tree items
	for i, tree := range trees {
		// Fetch all items for this tree
		itemQuery := `
			SELECT 
				gti.id,
				gti.parent_id,
				gti.title AS header_title,
				gti.host_id,
				h.description AS host_description,
				gti.local_graph_id,
				gtg.title_cache AS graph_title
			FROM graph_tree_items gti
			LEFT JOIN host h ON gti.host_id = h.id
			LEFT JOIN graph_templates_graph gtg ON gti.local_graph_id = gtg.local_graph_id
			WHERE gti.graph_tree_id = ?
			ORDER BY gti.parent_id, gti.position
		`
		rows, err := d.db.Query(itemQuery, tree.ID)
		if err != nil {
			return nil, fmt.Errorf("failed to query tree items: %w", err)
		}

		type rawItem struct {
			id           int
			parentID     int
			headerTitle  sql.NullString
			hostID       sql.NullInt64
			hostDesc     sql.NullString
			localGraphID sql.NullInt64
			graphTitle   sql.NullString
		}

		var rawItems []rawItem
		for rows.Next() {
			var ri rawItem
			err := rows.Scan(&ri.id, &ri.parentID, &ri.headerTitle, &ri.hostID, &ri.hostDesc, &ri.localGraphID, &ri.graphTitle)
			if err == nil {
				rawItems = append(rawItems, ri)
			}
		}
		rows.Close()

		// Map to store tree items by their item ID
		allNodes := make(map[int]*TreeItem)
		var rootNodes []*TreeItem

		for _, ri := range rawItems {
			node := &TreeItem{
				ID:       ri.id,
				ParentID: ri.parentID,
				Children: []TreeItem{},
			}

			if ri.localGraphID.Valid && ri.localGraphID.Int64 > 0 {
				node.Type = "graph"
				node.Title = ri.graphTitle.String
				node.LocalGraphID = int(ri.localGraphID.Int64)
			} else if ri.hostID.Valid && ri.hostID.Int64 > 0 {
				node.Type = "host"
				node.Title = ri.hostDesc.String
				node.HostID = int(ri.hostID.Int64)

				// If it's a Host node, Cacti automatically displays all graphs associated with this host
				hostGraphs, err := d.GetHostGraphs(node.HostID)
				if err == nil {
					for _, hg := range hostGraphs {
						node.Children = append(node.Children, TreeItem{
							ID:           0, // Dynamic item
							ParentID:     node.ID,
							Type:         "graph",
							Title:        hg.Title,
							LocalGraphID: hg.LocalGraphID,
							Children:     []TreeItem{},
						})
					}
				}
			} else {
				node.Type = "header"
				node.Title = ri.headerTitle.String
			}

			allNodes[node.ID] = node
		}

		// Connect children to parents
		for _, node := range allNodes {
			if node.ParentID == 0 {
				rootNodes = append(rootNodes, node)
			} else {
				parent, exists := allNodes[node.ParentID]
				if exists {
					parent.Children = append(parent.Children, *node)
				} else {
					rootNodes = append(rootNodes, node)
				}
			}
		}

		// Flatten root nodes back
		var finalItems []TreeItem
		for _, root := range rootNodes {
			finalItems = append(finalItems, *root)
		}
		trees[i].Items = finalItems
	}

	return trees, nil
}

// HostGraph represents a simplified graph mapping for host resolution.
type HostGraph struct {
	LocalGraphID int
	Title        string
}

// GetHostGraphs returns all graphs associated with a given Host ID.
func (d *DBConn) GetHostGraphs(hostID int) ([]HostGraph, error) {
	if d == nil || d.db == nil {
		return nil, nil
	}

	query := `
		SELECT gl.id, gtg.title_cache
		FROM graph_local gl
		INNER JOIN graph_templates_graph gtg ON gl.id = gtg.local_graph_id
		WHERE gl.host_id = ?
		ORDER BY gtg.title_cache
	`
	rows, err := d.db.Query(query, hostID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var graphs []HostGraph
	for rows.Next() {
		var hg HostGraph
		if err := rows.Scan(&hg.LocalGraphID, &hg.Title); err == nil {
			graphs = append(graphs, hg)
		}
	}
	return graphs, nil
}
