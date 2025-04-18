package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

// Config holds the configuration for the application
type Config struct {
	Port     int            `json:"port"`
	Interval time.Duration  `json:"interval"`
	Metrics  []MetricConfig `json:"metrics"`
	Database DatabaseConfig `json:"database"`
}

// DatabaseConfig holds the configuration for the database connection
type DatabaseConfig struct {
	Driver   string `json:"driver"`
	DSN      string `json:"dsn"`
	MaxOpen  int    `json:"max_open"`
	MaxIdle  int    `json:"max_idle"`
	Lifetime int    `json:"lifetime"`
}

// MetricConfig holds the configuration for a single metric
type MetricConfig struct {
	Name     string        `json:"name"`
	Query    string        `json:"query"`
	Interval time.Duration `json:"interval"`
}

// App holds the application state
type App struct {
	config     Config
	db         *sql.DB
	metrics    map[string]interface{}
	metricsMux sync.RWMutex
}

// NewApp creates a new instance of the App
func NewApp(config Config) (*App, error) {
	db, err := sql.Open(config.Database.Driver, config.Database.DSN)
	if err != nil {
		return nil, fmt.Errorf("error opening database: %w", err)
	}

	db.SetMaxOpenConns(config.Database.MaxOpen)
	db.SetMaxIdleConns(config.Database.MaxIdle)
	db.SetConnMaxLifetime(time.Duration(config.Database.Lifetime) * time.Second)

	app := &App{
		config:  config,
		db:      db,
		metrics: make(map[string]interface{}),
	}

	return app, nil
}

// Start starts the application
func (a *App) Start(ctx context.Context) error {
	// Start collecting metrics
	for _, metric := range a.config.Metrics {
		go a.collectMetric(ctx, metric)
	}

	// Start HTTP server
	http.HandleFunc("/metrics", a.handleMetrics)
	http.HandleFunc("/metrics.json", a.handleMetricsJSON)
	http.HandleFunc("/health", a.handleHealth)

	serverAddr := fmt.Sprintf(":%d", a.config.Port)
	log.Printf("Starting server on %s", serverAddr)
	return http.ListenAndServe(serverAddr, nil)
}

// collectMetric collects a single metric at the specified interval
func (a *App) collectMetric(ctx context.Context, metric MetricConfig) {
	ticker := time.NewTicker(metric.Interval)
	defer ticker.Stop()

	// Collect the metric immediately
	a.runQuery(metric)

	for {
		select {
		case <-ticker.C:
			a.runQuery(metric)
		case <-ctx.Done():
			return
		}
	}
}

// runQuery executes the metric's query and stores the result
func (a *App) runQuery(metric MetricConfig) {
	// Get column information
	rows, err := a.db.Query(metric.Query)
	if err != nil {
		log.Printf("Error executing query for metric %s: %v", metric.Name, err)
		return
	}
	defer rows.Close()

	// Get column names
	columns, err := rows.Columns()
	if err != nil {
		log.Printf("Error getting columns for metric %s: %v", metric.Name, err)
		return
	}

	// Prepare values slice for scanning
	valueIdx := -1
	for i, col := range columns {
		if col == "value" {
			valueIdx = i
			break
		}
	}

	if valueIdx == -1 {
		log.Printf("Error: metric %s query must include a 'value' column", metric.Name)
		return
	}

	// Create scan destinations
	values := make([]interface{}, len(columns))
	valuePtrs := make([]interface{}, len(columns))
	for i := range values {
		valuePtrs[i] = &values[i]
	}

	// Process each row of the result set
	a.metricsMux.Lock()
	defer a.metricsMux.Unlock()

	// Start with fresh metrics for this query
	// Use a prefix to identify metrics from this query
	prefix := metric.Name + "_"
	// First remove any existing metrics with this prefix
	for k := range a.metrics {
		if len(k) >= len(prefix) && k[:len(prefix)] == prefix {
			delete(a.metrics, k)
		}
	}

	for rows.Next() {
		// Scan the row into values
		if err := rows.Scan(valuePtrs...); err != nil {
			log.Printf("Error scanning row for metric %s: %v", metric.Name, err)
			continue
		}

		// Create labels
		labels := make(map[string]string)
		for i, col := range columns {
			if i == valueIdx {
				continue // Skip the value column
			}

			// Convert the value to string for label
			var labelValue string
			if values[i] == nil {
				labelValue = "null"
			} else {
				switch v := values[i].(type) {
				case []byte:
					labelValue = string(v)
				default:
					labelValue = fmt.Sprintf("%v", v)
				}
			}

			labels[col] = labelValue
		}

		// Create a unique metric name with labels
		if len(labels) > 0 {
			metricKeyName := prefix + buildLabelsKey(labels)
			a.metrics[metricKeyName] = map[string]interface{}{
				"value":  values[valueIdx],
				"labels": labels,
			}
		} else {
			// If no labels, use the metric name directly
			a.metrics[metric.Name] = values[valueIdx]
		}
	}

	if err := rows.Err(); err != nil {
		log.Printf("Error iterating rows for metric %s: %v", metric.Name, err)
	}

	log.Printf("Updated metric %s with %d time series", metric.Name, 1)
}

// buildLabelsKey creates a stable key from labels map
func buildLabelsKey(labels map[string]string) string {
	// Sort keys for stability
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	// Build key
	var b strings.Builder
	for i, k := range keys {
		if i > 0 {
			b.WriteString("_")
		}
		b.WriteString(k)
		b.WriteString("_")
		b.WriteString(labels[k])
	}
	return b.String()
}

// handleMetrics handles the /metrics endpoint for Prometheus
func (a *App) handleMetrics(w http.ResponseWriter, r *http.Request) {
	a.metricsMux.RLock()
	defer a.metricsMux.RUnlock()

	w.Header().Set("Content-Type", "text/plain")

	// Write metrics in Prometheus format
	for name, value := range a.metrics {
		var floatValue float64
		var labels map[string]string

		// Check if this is a labeled metric or a direct value
		if metricMap, ok := value.(map[string]interface{}); ok {
			// Get the value and labels from the map
			rawValue := metricMap["value"]
			if rawLabels, ok := metricMap["labels"].(map[string]string); ok {
				labels = rawLabels
			}

			// Convert value to float64
			switch v := rawValue.(type) {
			case int:
				floatValue = float64(v)
			case int32:
				floatValue = float64(v)
			case int64:
				floatValue = float64(v)
			case uint:
				floatValue = float64(v)
			case uint32:
				floatValue = float64(v)
			case uint64:
				floatValue = float64(v)
			case float32:
				floatValue = float64(v)
			case float64:
				floatValue = v
			case []byte:
				// Try to parse as float
				if f, err := strconv.ParseFloat(string(v), 64); err == nil {
					floatValue = f
				} else {
					// Skip non-numeric values
					log.Printf("Skipping non-numeric metric %s with value %v", name, v)
					continue
				}
			default:
				// Skip non-numeric values
				log.Printf("Skipping non-numeric metric %s with value type %T: %v", name, rawValue, rawValue)
				continue
			}
		} else {
			// Direct value (no labels)
			switch v := value.(type) {
			case int:
				floatValue = float64(v)
			case int32:
				floatValue = float64(v)
			case int64:
				floatValue = float64(v)
			case uint:
				floatValue = float64(v)
			case uint32:
				floatValue = float64(v)
			case uint64:
				floatValue = float64(v)
			case float32:
				floatValue = float64(v)
			case float64:
				floatValue = v
			case []byte:
				// Try to parse as float
				if f, err := strconv.ParseFloat(string(v), 64); err == nil {
					floatValue = f
				} else {
					// Skip non-numeric values
					log.Printf("Skipping non-numeric metric %s with value %v", name, v)
					continue
				}
			default:
				// Skip non-numeric values
				log.Printf("Skipping non-numeric metric %s with value type %T: %v", name, value, value)
				continue
			}
		}

		// Extract the base metric name (remove prefix and label encoding)
		baseName := name
		if idx := strings.Index(name, "_"); idx > 0 {
			baseName = name[:idx]
		}

		fmt.Fprintf(w, "# HELP %s Value from custom SQL query\n", baseName)
		fmt.Fprintf(w, "# TYPE %s gauge\n", baseName)

		// Format the metric line with labels if they exist
		if labels != nil && len(labels) > 0 {
			// Build the label string
			var labelStr strings.Builder
			labelStr.WriteString("{")

			first := true
			for k, v := range labels {
				if !first {
					labelStr.WriteString(",")
				}
				first = false
				labelStr.WriteString(k)
				labelStr.WriteString("=\"")
				labelStr.WriteString(escapeLabelValue(v))
				labelStr.WriteString("\"")
			}
			labelStr.WriteString("}")

			fmt.Fprintf(w, "%s%s %g\n", baseName, labelStr.String(), floatValue)
		} else {
			fmt.Fprintf(w, "%s %g\n", baseName, floatValue)
		}
	}
}

// escapeLabelValue escapes special characters in label values
func escapeLabelValue(value string) string {
	return strings.NewReplacer(
		"\\", "\\\\",
		"\n", "\\n",
		"\"", "\\\"",
	).Replace(value)
}

// handleMetricsJSON handles the /metrics.json endpoint
func (a *App) handleMetricsJSON(w http.ResponseWriter, r *http.Request) {
	a.metricsMux.RLock()
	defer a.metricsMux.RUnlock()

	w.Header().Set("Content-Type", "application/json")

	// Create a response structure that's more JSON-friendly
	response := make(map[string]interface{})

	for name, value := range a.metrics {
		if metricMap, ok := value.(map[string]interface{}); ok {
			// For metrics with labels, restructure them in a more JSON-friendly way
			baseName := name
			if idx := strings.Index(name, "_"); idx > 0 {
				baseName = name[:idx]
			}

			// Group metrics by base name
			var metrics []map[string]interface{}
			if existingMetrics, ok := response[baseName].([]map[string]interface{}); ok {
				metrics = existingMetrics
			} else {
				metrics = []map[string]interface{}{}
			}

			// Add this metric to the group
			metrics = append(metrics, map[string]interface{}{
				"value":  metricMap["value"],
				"labels": metricMap["labels"],
			})

			response[baseName] = metrics
		} else {
			// For direct values, just add them directly
			response[name] = value
		}
	}

	json.NewEncoder(w).Encode(response)
}

// handleHealth handles the /health endpoint
func (a *App) handleHealth(w http.ResponseWriter, r *http.Request) {
	// Check database connection
	err := a.db.Ping()
	if err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprintf(w, "Database connection error: %v", err)
		return
	}

	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, "OK")
}

func main() {
	configFile := flag.String("config", "", "Path to config file")
	flag.Parse()

	config, err := LoadConfig(*configFile)
	if err != nil {
		log.Fatalf("Error loading config: %v", err)
	}

	log.Printf("Starting application with %d metrics", len(config.Metrics))

	ctx := context.Background()
	app, err := NewApp(config)
	if err != nil {
		log.Fatalf("Error creating app: %v", err)
	}

	log.Fatal(app.Start(ctx))
}
