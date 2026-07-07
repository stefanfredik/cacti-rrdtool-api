package config

import (
	"encoding/json"
	"flag"
	"os"
	"os/exec"
	"strconv"
	"time"
)

// Config holds the application configuration settings.
type Config struct {
	ListenAddress        string        `json:"listen_address"`
	RRDDir               string        `json:"rrd_dir"`
	RRDToolCommand       string        `json:"rrdtool_command"`
	RRDToolTimeout       time.Duration `json:"rrdtool_timeout"`
	SignedQuerySecret    string        `json:"signed_query_secret"`
	BasicAuthUser        string        `json:"basic_auth_user"`
	BasicAuthPass        string        `json:"basic_auth_pass"`
	RefreshInterval      time.Duration `json:"refresh_interval"`
	DemoMode             bool          `json:"demo_mode"`
	TLSCertFile          string        `json:"tls_cert_file"`
	TLSKeyFile           string        `json:"tls_key_file"`
	RateLimitRPS         float64       `json:"rate_limit_rps"`
	RateLimitBurst       int           `json:"rate_limit_burst"`
	MaxConcurrentRRDTool int           `json:"max_concurrent_rrdtool"`
	DBHost               string        `json:"db_host"`
	DBPort               string        `json:"db_port"`
	DBUser               string        `json:"db_user"`
	DBPass               string        `json:"db_pass"`
	DBName               string        `json:"db_name"`
}

// LoadConfig loads configuration from CLI arguments, environment variables, and optionally a JSON config file.
func LoadConfig(args []string) (*Config, error) {
	cfg := &Config{
		ListenAddress:        "0.0.0.0:9191",
		RRDDir:               "/var/www/html/cacti/rra",
		RRDToolCommand:       "rrdtool",
		RRDToolTimeout:       30 * time.Second,
		RefreshInterval:      5 * time.Minute,
		DemoMode:             false,
		RateLimitRPS:         20.0,
		RateLimitBurst:       50,
		MaxConcurrentRRDTool: 10,
		DBHost:               "",
		DBPort:               "3306",
		DBUser:               "",
		DBPass:               "",
		DBName:               "",
	}

	// CLI Flags using a local FlagSet to avoid global redefinition panics in tests
	fs := flag.NewFlagSet("cacti-rrd-api", flag.ContinueOnError)
	configPath := fs.String("config", "", "Path to JSON configuration file")
	listenAddr := fs.String("listen", "", "Listen address (e.g., :9191)")
	rrdDir := fs.String("rrd-dir", "", "Directory containing .rrd files")
	rrdtoolBin := fs.String("rrdtool-bin", "", "Path to rrdtool binary")
	secret := fs.String("secret", "", "HMAC secret for signed queries")
	authUser := fs.String("auth-user", "", "Basic Auth username")
	authPass := fs.String("auth-pass", "", "Basic Auth password")
	demoMode := fs.Bool("demo", false, "Enable demo mode (runs without rrdtool binary)")
	tlsCert := fs.String("tls-cert", "", "Path to TLS certificate file")
	tlsKey := fs.String("tls-key", "", "Path to TLS private key file")
	rateRPS := fs.Float64("rate-rps", 0.0, "Rate limit requests per second (0 to disable)")
	rateBurst := fs.Int("rate-burst", 0, "Rate limit burst size")
	maxConns := fs.Int("max-conns", 0, "Maximum concurrent RRDTool CLI processes")
	dbHost := fs.String("db-host", "", "Cacti DB host (optional)")
	dbPort := fs.String("db-port", "3306", "Cacti DB port")
	dbUser := fs.String("db-user", "", "Cacti DB user")
	dbPass := fs.String("db-pass", "", "Cacti DB password")
	dbName := fs.String("db-name", "", "Cacti DB name")

	if err := fs.Parse(args); err != nil {
		return nil, err
	}

	// Load from JSON file if specified
	if *configPath != "" {
		file, err := os.Open(*configPath)
		if err != nil {
			return nil, err
		}
		defer file.Close()
		decoder := json.NewDecoder(file)
		if err := decoder.Decode(cfg); err != nil {
			return nil, err
		}
	}

	// Override from Environment Variables
	if val := os.Getenv("RRD_LISTEN_ADDRESS"); val != "" {
		cfg.ListenAddress = val
	}
	if val := os.Getenv("RRD_DIR"); val != "" {
		cfg.RRDDir = val
	}
	if val := os.Getenv("RRDTOOL_COMMAND"); val != "" {
		cfg.RRDToolCommand = val
	}
	if val := os.Getenv("RRD_SIGNED_QUERY_SECRET"); val != "" {
		cfg.SignedQuerySecret = val
	}
	if val := os.Getenv("RRD_BASIC_AUTH_USER"); val != "" {
		cfg.BasicAuthUser = val
	}
	if val := os.Getenv("RRD_BASIC_AUTH_PASS"); val != "" {
		cfg.BasicAuthPass = val
	}
	if val := os.Getenv("RRD_REFRESH_INTERVAL"); val != "" {
		if d, err := time.ParseDuration(val); err == nil {
			cfg.RefreshInterval = d
		}
	}
	if val := os.Getenv("RRD_DEMO_MODE"); val != "" {
		if b, err := strconv.ParseBool(val); err == nil {
			cfg.DemoMode = b
		}
	}
	if val := os.Getenv("RRD_TLS_CERT_FILE"); val != "" {
		cfg.TLSCertFile = val
	}
	if val := os.Getenv("RRD_TLS_KEY_FILE"); val != "" {
		cfg.TLSKeyFile = val
	}
	if val := os.Getenv("RRD_RATE_LIMIT_RPS"); val != "" {
		if f, err := strconv.ParseFloat(val, 64); err == nil {
			cfg.RateLimitRPS = f
		}
	}
	if val := os.Getenv("RRD_RATE_LIMIT_BURST"); val != "" {
		if i, err := strconv.Atoi(val); err == nil {
			cfg.RateLimitBurst = i
		}
	}
	if val := os.Getenv("RRD_MAX_CONCURRENT_RRDTOOL"); val != "" {
		if i, err := strconv.Atoi(val); err == nil {
			cfg.MaxConcurrentRRDTool = i
		}
	}
	if val := os.Getenv("RRD_DB_HOST"); val != "" {
		cfg.DBHost = val
	}
	if val := os.Getenv("RRD_DB_PORT"); val != "" {
		cfg.DBPort = val
	}
	if val := os.Getenv("RRD_DB_USER"); val != "" {
		cfg.DBUser = val
	}
	if val := os.Getenv("RRD_DB_PASS"); val != "" {
		cfg.DBPass = val
	}
	if val := os.Getenv("RRD_DB_NAME"); val != "" {
		cfg.DBName = val
	}

	// Override from Flags
	if *listenAddr != "" {
		cfg.ListenAddress = *listenAddr
	}
	if *rrdDir != "" {
		cfg.RRDDir = *rrdDir
	}
	if *rrdtoolBin != "" {
		cfg.RRDToolCommand = *rrdtoolBin
	}
	if *secret != "" {
		cfg.SignedQuerySecret = *secret
	}
	if *authUser != "" {
		cfg.BasicAuthUser = *authUser
	}
	if *authPass != "" {
		cfg.BasicAuthPass = *authPass
	}
	if *demoMode {
		cfg.DemoMode = true
	}
	if *tlsCert != "" {
		cfg.TLSCertFile = *tlsCert
	}
	if *tlsKey != "" {
		cfg.TLSKeyFile = *tlsKey
	}
	if *rateRPS > 0 {
		cfg.RateLimitRPS = *rateRPS
	}
	if *rateBurst > 0 {
		cfg.RateLimitBurst = *rateBurst
	}
	if *maxConns > 0 {
		cfg.MaxConcurrentRRDTool = *maxConns
	}
	if *dbHost != "" {
		cfg.DBHost = *dbHost
	}
	if *dbPort != "" && *dbPort != "3306" {
		cfg.DBPort = *dbPort
	}
	if *dbUser != "" {
		cfg.DBUser = *dbUser
	}
	if *dbPass != "" {
		cfg.DBPass = *dbPass
	}
	if *dbName != "" {
		cfg.DBName = *dbName
	}

	// Automatic Demo mode if rrdtool is not installed and demo was not explicitly disabled
	if !cfg.DemoMode {
		if _, err := os.Stat(cfg.RRDToolCommand); err != nil {
			// Check path search
			if !commandExists(cfg.RRDToolCommand) {
				cfg.DemoMode = true
			}
		}
	}

	return cfg, nil
}

func commandExists(cmd string) bool {
	_, err := exec.LookPath(cmd)
	return err == nil
}
