package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds all runtime configurations for argolum.
type Config struct {
	WatchNamespace      string
	ArgocdCMName        string
	ArgocdSecretName    string
	ArgocdRBACCMName    string
	LabelName           string
	LabelPartOf         string
	BcryptCost          int
	PruneOrphanUsers    bool
	PruneInterval       time.Duration
	IgnoreUsers         []string
	LogLevel            string
	LogFormat           string
	MetricsBindAddr     string
	HealthProbeBindAddr string
}

// Load loads the configuration from environment variables with sensible defaults.
func Load() *Config {
	cfg := &Config{
		WatchNamespace:      getEnv("WATCH_NAMESPACE", ""),
		ArgocdCMName:        getEnv("ARGOCD_CM_NAME", ""),
		ArgocdSecretName:    getEnv("ARGOCD_SECRET_NAME", ""),
		ArgocdRBACCMName:    getEnv("ARGOCD_RBAC_CM_NAME", ""),
		LabelName:           getEnv("LABEL_NAME", "argolum"),
		LabelPartOf:         getEnv("LABEL_PART_OF", "argocd"),
		BcryptCost:          getEnvInt("BCRYPT_COST", 10),
		PruneOrphanUsers:    getEnvBool("PRUNE_ORPHAN_USERS", false),
		PruneInterval:       getEnvDuration("PRUNE_INTERVAL", 10*time.Minute),
		IgnoreUsers:         getEnvSlice("IGNORE_USERS", []string{"admin"}),
		LogLevel:            getEnv("LOG_LEVEL", "info"),
		LogFormat:           getEnv("LOG_FORMAT", "json"),
		MetricsBindAddr:     getEnv("METRICS_BIND_ADDR", ":8080"),
		HealthProbeBindAddr: getEnv("HEALTH_PROBE_BIND_ADDR", ":8081"),
	}

	if cfg.WatchNamespace == "" {
		// Attempt to read namespace from in-cluster serviceaccount file
		if nsBytes, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace"); err == nil {
			cfg.WatchNamespace = strings.TrimSpace(string(nsBytes))
		}
	}

	return cfg
}

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return strings.TrimSpace(val)
	}
	return defaultVal
}

func getEnvInt(key string, defaultVal int) int {
	if val := os.Getenv(key); val != "" {
		if i, err := strconv.Atoi(strings.TrimSpace(val)); err == nil {
			return i
		}
	}
	return defaultVal
}

func getEnvBool(key string, defaultVal bool) bool {
	if val := os.Getenv(key); val != "" {
		b, err := strconv.ParseBool(strings.TrimSpace(val))
		if err == nil {
			return b
		}
	}
	return defaultVal
}

func getEnvDuration(key string, defaultVal time.Duration) time.Duration {
	if val := os.Getenv(key); val != "" {
		d, err := time.ParseDuration(strings.TrimSpace(val))
		if err == nil {
			return d
		}
	}
	return defaultVal
}

func getEnvSlice(key string, defaultVal []string) []string {
	if val := os.Getenv(key); val != "" {
		parts := strings.Split(val, ",")
		var res []string
		for _, p := range parts {
			trimmed := strings.TrimSpace(p)
			if trimmed != "" {
				res = append(res, trimmed)
			}
		}
		if len(res) > 0 {
			return res
		}
	}
	return defaultVal
}
