package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	AppEnv    string
	HTTP      HTTPConfig
	Database  DatabaseConfig
	Session   SessionConfig
	Storage   StorageConfig
	AdminSeed AdminSeedConfig
}

type HTTPConfig struct {
	Port int
}

type DatabaseConfig struct {
	DSN string
}

type SessionConfig struct {
	Secret     string
	TTL        time.Duration
	CookieName string
}

type StorageConfig struct {
	Endpoint  string
	Region    string
	Bucket    string
	AccessKey string
	SecretKey string
	UseSSL    bool
}

type AdminSeedConfig struct {
	Enabled     bool
	Username    string
	Password    string
	DisplayName string
}

type SafeSummary struct {
	AppEnv                       string
	Port                         int
	DatabaseConfigured           bool
	SessionCookieName            string
	SessionTTL                   string
	SessionSecretConfigured      bool
	StorageEndpoint              string
	StorageRegion                string
	StorageBucket                string
	StorageUseSSL                bool
	StorageCredentialsConfigured bool
	AdminSeedEnabled             bool
	AdminSeedUsername            string
	AdminSeedDisplayName         string
}

func Load(dotEnvPath string) (Config, error) {
	dotEnvValues := map[string]string{}
	if dotEnvPath != "" {
		values, err := ParseDotEnvFile(dotEnvPath)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				return Config{}, fmt.Errorf("parse %s: %w", dotEnvPath, err)
			}
		} else {
			dotEnvValues = values
		}
	}

	lookup := func(key string) (string, bool) {
		if value, ok := os.LookupEnv(key); ok {
			return value, true
		}
		value, ok := dotEnvValues[key]
		return value, ok
	}

	return loadWithLookup(lookup)
}

func loadWithLookup(lookup func(string) (string, bool)) (Config, error) {
	cfg := Config{
		AppEnv: "development",
		HTTP: HTTPConfig{
			Port: 8080,
		},
		Session: SessionConfig{
			TTL:        24 * time.Hour,
			CookieName: "secrete_amoo_session",
		},
		Storage: StorageConfig{
			Region: "us-east-1",
		},
		AdminSeed: AdminSeedConfig{
			Enabled: true,
		},
	}

	var errs []error

	if value, ok := lookup("APP_ENV"); ok && strings.TrimSpace(value) != "" {
		cfg.AppEnv = strings.TrimSpace(value)
	}

	if value, ok := lookup("PORT"); ok && strings.TrimSpace(value) != "" {
		port, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil || port < 1 || port > 65535 {
			errs = append(errs, fmt.Errorf("PORT must be an integer between 1 and 65535"))
		} else {
			cfg.HTTP.Port = port
		}
	}

	cfg.Database.DSN = readRequired("DB_DSN", lookup, &errs)
	cfg.Session.Secret = readRequired("SESSION_SECRET", lookup, &errs)

	if len(cfg.Session.Secret) > 0 && len(cfg.Session.Secret) < 32 {
		errs = append(errs, fmt.Errorf("SESSION_SECRET must be at least 32 characters"))
	}

	if value, ok := lookup("SESSION_TTL"); ok && strings.TrimSpace(value) != "" {
		ttl, err := time.ParseDuration(strings.TrimSpace(value))
		if err != nil || ttl <= 0 {
			errs = append(errs, fmt.Errorf("SESSION_TTL must be a valid positive duration"))
		} else {
			cfg.Session.TTL = ttl
		}
	}

	if value, ok := lookup("SESSION_COOKIE_NAME"); ok && strings.TrimSpace(value) != "" {
		cfg.Session.CookieName = strings.TrimSpace(value)
	}

	cfg.Storage.Endpoint = readRequired("STORAGE_ENDPOINT", lookup, &errs)
	cfg.Storage.Bucket = readRequired("STORAGE_BUCKET", lookup, &errs)
	cfg.Storage.AccessKey = readRequired("STORAGE_ACCESS_KEY", lookup, &errs)
	cfg.Storage.SecretKey = readRequired("STORAGE_SECRET_KEY", lookup, &errs)

	if value, ok := lookup("STORAGE_REGION"); ok && strings.TrimSpace(value) != "" {
		cfg.Storage.Region = strings.TrimSpace(value)
	}

	if value, ok := lookup("STORAGE_USE_SSL"); ok && strings.TrimSpace(value) != "" {
		useSSL, err := strconv.ParseBool(strings.TrimSpace(value))
		if err != nil {
			errs = append(errs, fmt.Errorf("STORAGE_USE_SSL must be true/false"))
		} else {
			cfg.Storage.UseSSL = useSSL
		}
	}

	if value, ok := lookup("ADMIN_SEED_ENABLED"); ok && strings.TrimSpace(value) != "" {
		enabled, err := strconv.ParseBool(strings.TrimSpace(value))
		if err != nil {
			errs = append(errs, fmt.Errorf("ADMIN_SEED_ENABLED must be true/false"))
		} else {
			cfg.AdminSeed.Enabled = enabled
		}
	}

	if cfg.AdminSeed.Enabled {
		cfg.AdminSeed.Username = readRequired("ADMIN_SEED_USERNAME", lookup, &errs)
		cfg.AdminSeed.Password = readRequired("ADMIN_SEED_PASSWORD", lookup, &errs)
		cfg.AdminSeed.DisplayName = readRequired("ADMIN_SEED_DISPLAY_NAME", lookup, &errs)

		if len(cfg.AdminSeed.Password) > 0 && len(cfg.AdminSeed.Password) < 8 {
			errs = append(errs, fmt.Errorf("ADMIN_SEED_PASSWORD must be at least 8 characters"))
		}
	}

	if len(errs) > 0 {
		return Config{}, errors.Join(errs...)
	}

	return cfg, nil
}

func (c Config) SafeSummary() SafeSummary {
	return SafeSummary{
		AppEnv:                       c.AppEnv,
		Port:                         c.HTTP.Port,
		DatabaseConfigured:           c.Database.DSN != "",
		SessionCookieName:            c.Session.CookieName,
		SessionTTL:                   c.Session.TTL.String(),
		SessionSecretConfigured:      c.Session.Secret != "",
		StorageEndpoint:              c.Storage.Endpoint,
		StorageRegion:                c.Storage.Region,
		StorageBucket:                c.Storage.Bucket,
		StorageUseSSL:                c.Storage.UseSSL,
		StorageCredentialsConfigured: c.Storage.AccessKey != "" && c.Storage.SecretKey != "",
		AdminSeedEnabled:             c.AdminSeed.Enabled,
		AdminSeedUsername:            c.AdminSeed.Username,
		AdminSeedDisplayName:         c.AdminSeed.DisplayName,
	}
}

func readRequired(key string, lookup func(string) (string, bool), errs *[]error) string {
	value, ok := lookup(key)
	if !ok || strings.TrimSpace(value) == "" {
		*errs = append(*errs, fmt.Errorf("%s is required", key))
		return ""
	}
	return strings.TrimSpace(value)
}
