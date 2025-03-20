# RefluxDB Query Documentation

This document outlines the query functionality implemented in RefluxDB and compares it with InfluxDB's specification.

## Overview

RefluxDB implements a subset of InfluxDB's query language, focusing on the most commonly used features. The implementation supports both HTTP and UDP protocols for data ingestion and querying.

## Supported Query Features

### Basic Query Syntax

```sql
SELECT [field_key | *] FROM measurement [WHERE condition] [GROUP BY tag_key]
```

### Supported Commands

1. `SELECT` - Query data from measurements
2. `SHOW MEASUREMENTS` - List all measurements in the database
3. `SHOW DATABASES` - List all databases (currently returns a static list)

### Supported Aggregation Functions

1. `mean()` - Calculates the arithmetic mean of values
2. `sum()` - Calculates the sum of values
3. `count()` - Counts the number of values
4. `min()` - Returns the minimum value
5. `max()` - Returns the maximum value

### Time Range Support

- Queries support time range filtering through the HTTP API using `start` and `end` parameters
- Time values are in Unix nanoseconds
- Default range is from 0 to current time if not specified

### Field Selection

- Supports selecting specific fields or all fields using `*`
- Fields can be selected with or without aggregation functions

### Group By Support

- Supports grouping by tags (currently implemented for host tag)
- Aggregation functions work with grouped data

## API Endpoints

### HTTP API

1. `/api/v2/query` (GET/POST)
   - Parameters:
     - `org`: Organization name (required)
     - `bucket`: Bucket name (required)
     - `measurement`: Measurement name (required)
     - `start`: Start time in Unix nanoseconds (optional)
     - `end`: End time in Unix nanoseconds (optional)

2. `/query` (GET/POST) - InfluxDB v1 API compatibility
   - Parameters:
     - `db`: Database name (required)
     - `q`: Query string (required)
     - `epoch`: Time format (ms, s, u, ns) (optional)

### Response Format

```json
{
  "results": [
    {
      "statement_id": 0,
      "series": [
        {
          "name": "measurement_name",
          "columns": ["time", "field", "value"],
          "values": [
            [timestamp, field_name, value],
            ...
          ]
        }
      ]
    }
  ]
}
```

## Coverage Analysis

### Implemented Features

1. Basic SELECT queries
2. Field selection
3. Basic aggregation functions
4. Time range filtering
5. Tag-based grouping
6. InfluxDB v1 and v2 API compatibility
7. Support for all data types (integer, float, string, boolean)
8. SHOW MEASUREMENTS command
9. SHOW DATABASES command (static implementation)

### Missing Features

1. Complex WHERE clauses
2. Advanced aggregation functions (percentile, std. dev., etc.)
3. Continuous queries
4. Subqueries
5. Regular expressions in field/tag selection
6. Time-based grouping (GROUP BY time())
7. Fill options for missing data
8. OFFSET and LIMIT clauses
9. ORDER BY clause
10. INTO clause for query results
11. Dynamic SHOW DATABASES implementation
12. SHOW SERIES command
13. SHOW RETENTION POLICIES command
14. SHOW SHARDS command

## Example Queries

### Basic Select
```sql
SELECT value FROM cpu
```

### Select with Aggregation
```sql
SELECT mean(value) FROM cpu GROUP BY host
```

### Select All Fields
```sql
SELECT * FROM cpu
```

### Show Measurements
```sql
SHOW MEASUREMENTS
```

### Show Databases
```sql
SHOW DATABASES
```

## Limitations

1. No support for complex mathematical operations
2. Limited to basic aggregation functions
3. No support for subqueries or joins
4. No support for regular expressions in field/tag selection
5. No support for time-based grouping
6. No support for continuous queries
7. No support for INTO clause
8. No support for OFFSET/LIMIT
9. No support for ORDER BY
10. SHOW DATABASES returns static list
11. No support for SHOW SERIES
12. No support for SHOW RETENTION POLICIES
13. No support for SHOW SHARDS

## Future Improvements

1. Implement complex WHERE clauses
2. Add more aggregation functions
3. Add support for time-based grouping
4. Add support for regular expressions
5. Implement continuous queries
6. Add support for subqueries
7. Add support for INTO clause
8. Add support for OFFSET/LIMIT
9. Add support for ORDER BY
10. Improve error handling and validation
11. Implement dynamic SHOW DATABASES
12. Add SHOW SERIES command
13. Add SHOW RETENTION POLICIES command
14. Add SHOW SHARDS command

## Comparison with InfluxDB

RefluxDB implements approximately 30% of InfluxDB's query functionality, focusing on the most commonly used features. The implementation prioritizes:

1. Basic query operations
2. Simple aggregations
3. Time range filtering
4. Tag-based grouping
5. API compatibility
6. Basic SHOW commands

While this covers the most common use cases, it may not be suitable for complex analytics or advanced querying scenarios that require the full feature set of InfluxDB. 