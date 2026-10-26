package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// Connection describes a single RabbitMQ broker endpoint pair:
// the HTTP management API and the AMQP 0-9-1 port of the same broker.
type Connection struct {
	Name     string `json:"name"`
	MgmtURL  string `json:"mgmt_url"`
	AMQPURL  string `json:"amqp_url"`
	Username string `json:"username"`
	Password string `json:"password"`
}

// Config is the resolved application configuration.
type Config struct {
	Addr        string
	ReadOnly    bool
	SchemasDir  string
	Connections []Connection
}

// Env prefix used for all environment variables.
const envPrefix = "RMQ_CLUE_"

// Load builds configuration from environment variables and, when set,
// the JSON file pointed to by RMQ_CLUE_CONNECTIONS_FILE. Credentials are
// accepted only via environment or file, never via command-line flags,
// so they stay out of shell history and process listings.
func Load() (*Config, error) {
	cfg := &Config{
		Addr:        envString("ADDR", "127.0.0.1:8080"),
		ReadOnly:    envBool("READ_ONLY", false),
		SchemasDir:  envString("SCHEMAS_DIR", "data/schemas"),
		Connections: []Connection{},
	}

	if path := envString("CONNECTIONS_FILE", ""); path != "" {
		file, err := loadFile(path)
		if err != nil {
			return nil, err
		}
		cfg.Connections = append(cfg.Connections, file...)
	}

	if mgmt := envString("MGMT_URL", ""); mgmt != "" || envString("AMQP_URL", "") != "" {
		def := Connection{
			Name:     envString("CONNECTION_NAME", "default"),
			MgmtURL:  mgmt,
			AMQPURL:  envString("AMQP_URL", ""),
			Username: envString("USER", ""),
			Password: envString("PASS", ""),
		}
		if def.MgmtURL == "" || def.AMQPURL == "" {
			return nil, fmt.Errorf("%sMGMT_URL and %sAMQP_URL must both be set", envPrefix, envPrefix)
		}
		cfg.Connections = append(cfg.Connections, def)
	}

	if len(cfg.Connections) == 0 {
		return nil, fmt.Errorf(
			"no connections configured: set %sMGMT_URL/%sAMQP_URL or %sCONNECTIONS_FILE",
			envPrefix, envPrefix, envPrefix,
		)
	}
	for i := range cfg.Connections {
		if cfg.Connections[i].Name == "" {
			cfg.Connections[i].Name = fmt.Sprintf("connection-%d", i+1)
		}
	}
	return cfg, nil
}

func loadFile(path string) ([]Connection, error) {
	raw, err := os.ReadFile(path) //nolint:gosec // path comes from the operator-provided env/file config
	if err != nil {
		return nil, fmt.Errorf("read connections file: %w", err)
	}
	var conns []Connection
	if err := json.Unmarshal(raw, &conns); err != nil {
		return nil, fmt.Errorf("parse connections file: %w", err)
	}
	return conns, nil
}

func envString(key, def string) string {
	v := strings.TrimSpace(os.Getenv(envPrefix + key))
	if v == "" {
		return def
	}
	return v
}

func envBool(key string, def bool) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(envPrefix + key))) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return def
	}
}
