#!/bin/bash

# Colors for output
GREEN='\033[0;32m'
RED='\033[0;31m'
NC='\033[0m' # No Color
YELLOW='\033[1;33m'

echo -e "${YELLOW}Starting RefluxDB Test Suite${NC}"

# Check if ports are available
check_port() {
    if lsof -i:$1 > /dev/null; then
        echo -e "${RED}Error: Port $1 is in use. Please free this port before running tests.${NC}"
        exit 1
    fi
}

echo "Checking required ports..."
check_port 8086
check_port 8089

# Run different test configurations
echo -e "\n${YELLOW}Running protocol tests...${NC}"
go test -v ./internal/protocol/...

echo -e "\n${YELLOW}Running server tests...${NC}"
go test -v ./internal/server/...

echo -e "\n${YELLOW}Running UDP server tests...${NC}"
go test -v ./internal/udp/...

echo -e "\n${YELLOW}Running end-to-end tests...${NC}"
go test -v ./tests -run TestEndToEnd

echo -e "\n${YELLOW}Running InfluxDB compatibility tests...${NC}"
go test -v ./tests -run TestInfluxDBCompatibility

echo -e "\n${YELLOW}Running load tests...${NC}"
go test -v ./tests -run "TestConcurrent|TestQuery" -timeout 5m

# Check if any tests failed
if [ $? -eq 0 ]; then
    echo -e "\n${GREEN}All tests completed successfully!${NC}"
else
    echo -e "\n${RED}Some tests failed. Please check the output above for details.${NC}"
    exit 1
fi 