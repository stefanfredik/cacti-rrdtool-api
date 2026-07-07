package config

import (
	"os"
	"testing"
	"time"
)

func TestLoadConfigDefaults(t *testing.T) {
	// Clear relevant env variables to test default loading
	envVars := []string{
		"RRD_LISTEN_ADDRESS", "RRD_DIR", "RRDTOOL_COMMAND",
		"RRD_SIGNED_QUERY_SECRET", "RRD_BASIC_AUTH_USER",
		"RRD_BASIC_AUTH_PASS", "RRD_REFRESH_INTERVAL", "RRD_DEMO_MODE",
	}
	for _, env := range envVars {
		os.Unsetenv(env)
	}

	cfg, err := LoadConfig(nil)
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	if cfg.ListenAddress != "0.0.0.0:9292" {
		t.Errorf("Expected default listen address 0.0.0.0:9292, got %s", cfg.ListenAddress)
	}
	if cfg.RRDDir != "/var/www/html/cacti/rra" {
		t.Errorf("Expected default RRD dir /var/www/html/cacti/rra, got %s", cfg.RRDDir)
	}
	if cfg.RRDToolCommand != "rrdtool" {
		t.Errorf("Expected default rrdtool bin rrdtool, got %s", cfg.RRDToolCommand)
	}
	if cfg.RefreshInterval != 5*time.Minute {
		t.Errorf("Expected default refresh interval 5m, got %s", cfg.RefreshInterval)
	}
}

func TestLoadConfigEnvOverrides(t *testing.T) {
	os.Setenv("RRD_LISTEN_ADDRESS", "127.0.0.1:8080")
	os.Setenv("RRD_DIR", "/tmp/cacti-test/rra")
	os.Setenv("RRDTOOL_COMMAND", "/usr/local/bin/rrdtool")
	os.Setenv("RRD_SIGNED_QUERY_SECRET", "secret123")
	os.Setenv("RRD_BASIC_AUTH_USER", "admin")
	os.Setenv("RRD_BASIC_AUTH_PASS", "pass123")
	os.Setenv("RRD_REFRESH_INTERVAL", "1m")
	os.Setenv("RRD_DEMO_MODE", "true")

	defer func() {
		os.Unsetenv("RRD_LISTEN_ADDRESS")
		os.Unsetenv("RRD_DIR")
		os.Unsetenv("RRDTOOL_COMMAND")
		os.Unsetenv("RRD_SIGNED_QUERY_SECRET")
		os.Unsetenv("RRD_BASIC_AUTH_USER")
		os.Unsetenv("RRD_BASIC_AUTH_PASS")
		os.Unsetenv("RRD_REFRESH_INTERVAL")
		os.Unsetenv("RRD_DEMO_MODE")
	}()

	cfg, err := LoadConfig(nil)
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	if cfg.ListenAddress != "127.0.0.1:8080" {
		t.Errorf("Expected listen address 127.0.0.1:8080, got %s", cfg.ListenAddress)
	}
	if cfg.RRDDir != "/tmp/cacti-test/rra" {
		t.Errorf("Expected RRD dir /tmp/cacti-test/rra, got %s", cfg.RRDDir)
	}
	if cfg.RRDToolCommand != "/usr/local/bin/rrdtool" {
		t.Errorf("Expected rrdtool bin /usr/local/bin/rrdtool, got %s", cfg.RRDToolCommand)
	}
	if cfg.SignedQuerySecret != "secret123" {
		t.Errorf("Expected secret secret123, got %s", cfg.SignedQuerySecret)
	}
	if cfg.BasicAuthUser != "admin" {
		t.Errorf("Expected basic auth user admin, got %s", cfg.BasicAuthUser)
	}
	if cfg.BasicAuthPass != "pass123" {
		t.Errorf("Expected basic auth pass pass123, got %s", cfg.BasicAuthPass)
	}
	if cfg.RefreshInterval != 1*time.Minute {
		t.Errorf("Expected refresh interval 1m, got %s", cfg.RefreshInterval)
	}
	if !cfg.DemoMode {
		t.Errorf("Expected DemoMode true, got false")
	}
}

func TestLoadConfigArgsOverrides(t *testing.T) {
	// Clear env vars to avoid interference
	os.Unsetenv("RRD_LISTEN_ADDRESS")
	os.Unsetenv("RRD_DIR")
	os.Unsetenv("RRDTOOL_COMMAND")

	args := []string{
		"-listen", "127.0.0.1:9292",
		"-rrd-dir", "/custom/rra",
		"-rrdtool-bin", "/bin/rrdtool",
		"-secret", "supersecret",
		"-auth-user", "cacti",
		"-auth-pass", "securepass",
		"-demo",
	}

	cfg, err := LoadConfig(args)
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	if cfg.ListenAddress != "127.0.0.1:9292" {
		t.Errorf("Expected listen address 127.0.0.1:9292, got %s", cfg.ListenAddress)
	}
	if cfg.RRDDir != "/custom/rra" {
		t.Errorf("Expected RRD dir /custom/rra, got %s", cfg.RRDDir)
	}
	if cfg.RRDToolCommand != "/bin/rrdtool" {
		t.Errorf("Expected rrdtool bin /bin/rrdtool, got %s", cfg.RRDToolCommand)
	}
	if cfg.SignedQuerySecret != "supersecret" {
		t.Errorf("Expected secret supersecret, got %s", cfg.SignedQuerySecret)
	}
	if cfg.BasicAuthUser != "cacti" {
		t.Errorf("Expected basic auth user cacti, got %s", cfg.BasicAuthUser)
	}
	if cfg.BasicAuthPass != "securepass" {
		t.Errorf("Expected basic auth pass securepass, got %s", cfg.BasicAuthPass)
	}
	if !cfg.DemoMode {
		t.Errorf("Expected DemoMode true, got false")
	}
}
