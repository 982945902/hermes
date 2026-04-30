package config

import (
	"bufio"
	"os"
	"strings"
	"time"
)

type Config struct {
	Port              string
	MongoURI          string
	MongoDatabase     string
	AdminUsername     string
	AdminPassword     string
	AdminPasswordHash string
	JWTSecret         string
	GatewayAPIKey     string
	FrontendDist      string
	UpstreamTimeout   time.Duration
	IdentityName      string
	IdentityEnabled   bool
}

func Load() Config {
	loadDotEnv(".env")

	return Config{
		Port:              env("PORT", "3000"),
		MongoURI:          env("MONGO_URI", "mongodb://localhost:27017"),
		MongoDatabase:     env("MONGO_DATABASE", "hermes"),
		AdminUsername:     env("ADMIN_USERNAME", "admin"),
		AdminPassword:     env("ADMIN_PASSWORD", "admin123"),
		AdminPasswordHash: os.Getenv("ADMIN_PASSWORD_HASH"),
		JWTSecret:         env("JWT_SECRET", "change-me"),
		GatewayAPIKey:     env("GATEWAY_API_KEY", ""),
		FrontendDist:      env("FRONTEND_DIST", "web/dist"),
		UpstreamTimeout:   120 * time.Second,
		IdentityName:      env("IDENTITY_NAME", "Hermes AI"),
		IdentityEnabled:   envBool("IDENTITY_ENABLED", true),
	}
}

func env(key string, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func envBool(key string, fallback bool) bool {
	value := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	if value == "" {
		return fallback
	}
	return value == "1" || value == "true" || value == "yes" || value == "on"
}

func loadDotEnv(path string) {
	file, err := os.Open(path)
	if err != nil {
		return
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		if key != "" && os.Getenv(key) == "" {
			_ = os.Setenv(key, value)
		}
	}
}
