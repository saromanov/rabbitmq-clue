package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadFromEnv(t *testing.T) {
	t.Setenv("RMQ_CLUE_MGMT_URL", "http://localhost:15672")
	t.Setenv("RMQ_CLUE_AMQP_URL", "amqp://localhost:5672")
	t.Setenv("RMQ_CLUE_USER", "guest")
	t.Setenv("RMQ_CLUE_PASS", "guest")
	t.Setenv("RMQ_CLUE_ADDR", "")
	t.Setenv("RMQ_CLUE_READ_ONLY", "true")
	os.Unsetenv("RMQ_CLUE_CONNECTIONS_FILE")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Addr != "127.0.0.1:8080" {
		t.Fatalf("addr: %q", cfg.Addr)
	}
	if !cfg.ReadOnly {
		t.Fatal("want read-only")
	}
	if len(cfg.Connections) != 1 || cfg.Connections[0].Name != "default" {
		t.Fatalf("connections: %+v", cfg.Connections)
	}
}

func TestLoadPartialEnvFails(t *testing.T) {
	t.Setenv("RMQ_CLUE_MGMT_URL", "http://localhost:15672")
	os.Unsetenv("RMQ_CLUE_AMQP_URL")
	os.Unsetenv("RMQ_CLUE_CONNECTIONS_FILE")
	if _, err := Load(); err == nil {
		t.Fatal("want error when AMQP_URL missing")
	}
}

func TestLoadConnectionsFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "conns.json")
	content := `[
		{"name":"prod","mgmt_url":"https://rmq-prod:15672","amqp_url":"amqps://rmq-prod:5671","username":"svc","password":"s3cret"},
		{"mgmt_url":"https://rmq-dev:15672","amqp_url":"amqp://rmq-dev:5672"}
	]`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	t.Setenv("RMQ_CLUE_CONNECTIONS_FILE", path)
	os.Unsetenv("RMQ_CLUE_MGMT_URL")
	os.Unsetenv("RMQ_CLUE_AMQP_URL")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Connections) != 2 {
		t.Fatalf("connections: %+v", cfg.Connections)
	}
	if cfg.Connections[1].Name != "connection-2" {
		t.Fatalf("auto name: %q", cfg.Connections[1].Name)
	}
	if cfg.Connections[0].Password == "" {
		t.Fatal("password lost")
	}
}

func TestLoadNothingFails(t *testing.T) {
	os.Unsetenv("RMQ_CLUE_MGMT_URL")
	os.Unsetenv("RMQ_CLUE_AMQP_URL")
	os.Unsetenv("RMQ_CLUE_CONNECTIONS_FILE")
	if _, err := Load(); err == nil {
		t.Fatal("want error without any connection")
	}
}
