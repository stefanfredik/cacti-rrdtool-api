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
