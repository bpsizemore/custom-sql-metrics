package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"time"
)

// jsonConfig is used to unmarshal the JSON configuration file
type jsonConfig struct {
	Port     int                `json:"port"`
	Interval string             `json:"interval"`
	Metrics  []jsonMetricConfig `json:"metrics"`
	Database DatabaseConfig     `json:"database"`
}

// jsonMetricConfig is used to unmarshal the metric configuration
type jsonMetricConfig struct {
	Name     string `json:"name"`
	Query    string `json:"query"`
	Interval string `json:"interval"`
}

// LoadConfig loads the application configuration from a file
func LoadConfig(path string) (Config, error) {
	config := Config{
		Port:     8080,
		Interval: 60 * time.Second,
		Database: DatabaseConfig{
			Driver:   "mysql",
			DSN:      "user:password@tcp(host:3306)/database",
			MaxOpen:  10,
			MaxIdle:  5,
			Lifetime: 300,
		},
		Metrics: []MetricConfig{},
	}

	// Load from file if it exists
	if path != "" {
		file, err := os.Open(path)
		if err != nil {
			if !os.IsNotExist(err) {
				return config, fmt.Errorf("error opening config file: %w", err)
			}
			// File doesn't exist, we'll use environment variables or defaults
		} else {
			defer file.Close()

			var jsonCfg jsonConfig
			decoder := json.NewDecoder(file)
			if err := decoder.Decode(&jsonCfg); err != nil {
				return config, fmt.Errorf("error decoding config file: %w", err)
			}

			// Convert JSON config to application config
			config.Port = jsonCfg.Port

			if interval, err := time.ParseDuration(jsonCfg.Interval); err == nil {
				config.Interval = interval
			}

			config.Database = jsonCfg.Database

			// Convert metric configs
			for _, jsonMetric := range jsonCfg.Metrics {
				metric := MetricConfig{
					Name:  jsonMetric.Name,
					Query: jsonMetric.Query,
				}

				if interval, err := time.ParseDuration(jsonMetric.Interval); err == nil {
					metric.Interval = interval
				} else {
					// Default to global interval if parsing fails
					metric.Interval = config.Interval
				}

				config.Metrics = append(config.Metrics, metric)
			}
		}
	}

	// Override with environment variables if they exist
	if port := os.Getenv("PORT"); port != "" {
		if p, err := strconv.Atoi(port); err == nil {
			config.Port = p
		}
	}

	if interval := os.Getenv("INTERVAL"); interval != "" {
		if i, err := time.ParseDuration(interval); err == nil {
			config.Interval = i
		}
	}

	if driver := os.Getenv("DB_DRIVER"); driver != "" {
		config.Database.Driver = driver
	}

	if dsn := os.Getenv("DB_DSN"); dsn != "" {
		config.Database.DSN = dsn
	}

	if maxOpen := os.Getenv("DB_MAX_OPEN"); maxOpen != "" {
		if mo, err := strconv.Atoi(maxOpen); err == nil {
			config.Database.MaxOpen = mo
		}
	}

	if maxIdle := os.Getenv("DB_MAX_IDLE"); maxIdle != "" {
		if mi, err := strconv.Atoi(maxIdle); err == nil {
			config.Database.MaxIdle = mi
		}
	}

	if lifetime := os.Getenv("DB_LIFETIME"); lifetime != "" {
		if lt, err := strconv.Atoi(lifetime); err == nil {
			config.Database.Lifetime = lt
		}
	}

	return config, nil
}
