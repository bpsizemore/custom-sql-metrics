# Custom SQL Metrics

A Go webserver that runs SQL queries at configurable intervals and exposes the results via HTTP endpoints in both Prometheus-compatible and JSON formats.

## Features

- Run custom SQL queries at different intervals
- Expose metrics in Prometheus-compatible format at `/metrics`
- Expose metrics in JSON format at `/metrics.json`
- Health check endpoint at `/health`
- Configuration via JSON file or environment variables
- Support for multi-dimensional metrics with labels

## Installation

### Prerequisites

- Go 1.16 or later
- Access to a SQL database (MySQL supported by default)

### Building from source

```bash
git clone https://github.com/bpsizemore/custom-sql-metrics.git
cd custom-sql-metrics
go build
```

## Usage

### Basic Usage

1. Create a configuration file (see [Configuration](#configuration) below)
2. Run the application:

```bash
./custom-sql-metrics --config config.json
```

### Configuration

Configuration can be provided via a JSON file or environment variables.

#### Example Configuration File

See the `config.json.sample` file for a complete example. Here's a basic configuration:

```json
{
  "port": 8080,
  "interval": "1m",
  "database": {
    "driver": "mysql",
    "dsn": "user:password@tcp(localhost:3306)/database",
    "max_open": 10,
    "max_idle": 5,
    "lifetime": 300
  },
  "metrics": [
    {
      "name": "active_users",
      "query": "SELECT COUNT(*) as value FROM users WHERE last_active > DATE_SUB(NOW(), INTERVAL 15 MINUTE)",
      "interval": "1m"
    }
  ]
}
```

#### Creating Multi-dimensional Metrics with Labels

You can create multi-dimensional metrics by including multiple columns in your query. The column named `value` will be used as the metric value, and all other columns will become labels.

Example:

```json
{
  "name": "users_by_status",
  "query": "SELECT status, COUNT(*) as value FROM users GROUP BY status",
  "interval": "5m"
}
```

This will produce metrics like:

```
users_by_status{status="active"} 150
users_by_status{status="inactive"} 75
users_by_status{status="suspended"} 25
```

You can include multiple label dimensions:

```json
{
  "name": "orders_by_status_and_payment",
  "query": "SELECT status, payment_method, COUNT(*) as value FROM orders GROUP BY status, payment_method",
  "interval": "2m"
}
```

This will produce metrics like:

```
orders_by_status_and_payment{status="pending",payment_method="credit_card"} 42
orders_by_status_and_payment{status="pending",payment_method="paypal"} 28
orders_by_status_and_payment{status="shipped",payment_method="credit_card"} 65
```

**Important**: Your query must include a column named `value` which will be used as the metric value.

#### Environment Variables

The following environment variables can be used to override the configuration:

- `PORT`: Server port
- `INTERVAL`: Default interval for metrics collection (e.g., "30s", "1m", "5m")
- `DB_DRIVER`: Database driver (e.g., "mysql")
- `DB_DSN`: Database connection string
- `DB_MAX_OPEN`: Maximum number of open connections
- `DB_MAX_IDLE`: Maximum number of idle connections
- `DB_LIFETIME`: Maximum lifetime of connections in seconds

### Endpoints

- `/metrics`: Returns metrics in Prometheus-compatible format
- `/metrics.json`: Returns metrics in JSON format
- `/health`: Returns "OK" if the application is healthy

## Using with Prometheus

Add the following to your `prometheus.yml` configuration:

```yaml
scrape_configs:
  - job_name: 'custom-sql-metrics'
    scrape_interval: 15s
    static_configs:
      - targets: ['localhost:8080']
```

## Using with Grafana

1. Configure Prometheus as a data source in Grafana
2. Create a dashboard using the metrics exposed by this application

## License

MIT
