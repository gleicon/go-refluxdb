# Go RefluxDB

The cure for timeseries GERD (gastro etc reflux disease). A lightweight InfluxDB-compatible time series database written in Go, which implements a subset of the InfluxDB HTTP and UDP APIs, making it compatible with existing InfluxDB clients and tools like Grafana.

## Features

- InfluxDB v1 and v2 HTTP API compatibility
- UDP protocol support for data ingestion
- SQLite-based storage backend
- Support for line protocol data format
- Query support for:
  - Basic SELECT queries
  - Aggregation functions (mean, sum, count, min, max)
  - Time-based queries with millisecond precision
  - GROUP BY time intervals
  - Tag support

## Installation

```bash
# Clone the repository
git clone https://github.com/gleicon/go-refluxdb.git
cd go-refluxdb

# Build the project
make build

# Run the server
make run
```

## Usage

### Starting the Server

The server starts on port 8086 for HTTP and 8089 for UDP by default. You can configure these ports in the configuration.

```bash
# Start with default configuration
./build/refluxdb

# Or specify custom ports
./build/refluxdb --http-port 8086 --udp-port 8089
```

### Writing Data

#### HTTP API (v2)

```bash
curl -X POST "http://localhost:8086/api/v2/write?org=my-org&bucket=my-bucket" \
  --data-binary "cpu,host=server1 value=42.5 1465839830100400200"
```

#### HTTP API (v1)

```bash
curl -X POST "http://localhost:8086/write?db=mydb" \
  --data-binary "cpu,host=server1 value=42.5 1465839830100400200"
```

#### UDP Protocol

```bash
echo "cpu,host=server1 value=42.5 1465839830100400200" | nc -u localhost 8089
```

### Querying Data

#### HTTP API (v2)

```bash
curl -G "http://localhost:8086/api/v2/query" \
  --data-urlencode "org=my-org" \
  --data-urlencode "bucket=my-bucket" \
  --data-urlencode "measurement=cpu"
```

#### HTTP API (v1)

```bash
curl -G "http://localhost:8086/query" \
  --data-urlencode "db=mydb" \
  --data-urlencode "q=SELECT mean(\"value\") FROM \"cpu\" WHERE time >= now() - 1h GROUP BY time(5m) fill(null)"
```

### Grafana Integration

1. Add a new InfluxDB data source in Grafana
2. Set the URL to `http://localhost:8086`
3. Set the database name
4. Test the connection

### Using the InfluxDB CLI

The InfluxDB CLI can be used to interact with RefluxDB. Here's a complete example session:

```bash
# Connect to the database
influx -host localhost -port 8086

# Once connected, you can run commands:
> CREATE DATABASE mydb
> USE mydb

# Insert some data points
> INSERT cpu,host=server1 value=42.5
> INSERT cpu,host=server2 value=85.0
> INSERT memory,host=server1 used=75.5,free=24.5
> INSERT memory,host=server2 used=82.3,free=17.7

# Query the data
> SELECT * FROM cpu
> SELECT mean("value") FROM cpu WHERE time >= now() - 1h GROUP BY time(5m) fill(null)
> SELECT * FROM memory WHERE "host" = 'server1'

# Show available databases
> SHOW DATABASES

# Show available measurements
> SHOW MEASUREMENTS

# Show series
> SHOW SERIES

# Show tag keys
> SHOW TAG KEYS

# Show field keys
> SHOW FIELD KEYS

# Exit the CLI
> exit
```

Note: The InfluxDB CLI needs to be installed separately. You can download it from the [InfluxDB downloads page](https://portal.influxdata.com/downloads/).

## Development

### Prerequisites

- Go 1.23 or later
- Make
- SQLite3

### Building

```bash
# Build the project
make build

# Run tests
make test

# Run with coverage
make test-coverage

# Format code
make fmt

# Run linter
make lint
```

### Project Structure

```
.
├── cmd/
│   └── refluxdb/          # Main application entry point
├── internal/
│   ├── persistence/       # Database layer
│   ├── protocol/         # Line protocol parser
│   ├── server/          # HTTP server implementation
│   └── udp/             # UDP server implementation
└── tests/               # Integration tests
```

## License

This project is licensed under the MIT License - see the LICENSE file for details.

## Contributing

1. Fork the repository
2. Create your feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add some amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request
