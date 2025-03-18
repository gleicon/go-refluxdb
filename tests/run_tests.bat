@echo off
setlocal

echo Starting RefluxDB Test Suite

:: Check if ports are available (Windows specific)
netstat -an | findstr ":8086" > nul
if %ERRORLEVEL% EQU 0 (
    echo Error: Port 8086 is in use. Please free this port before running tests.
    exit /b 1
)

netstat -an | findstr ":8089" > nul
if %ERRORLEVEL% EQU 0 (
    echo Error: Port 8089 is in use. Please free this port before running tests.
    exit /b 1
)

:: Run tests
echo.
echo Running protocol tests...
go test -v ../internal/protocol/...
if %ERRORLEVEL% NEQ 0 goto :error

echo.
echo Running server tests...
go test -v ../internal/server/...
if %ERRORLEVEL% NEQ 0 goto :error

echo.
echo Running UDP server tests...
go test -v ../internal/udp/...
if %ERRORLEVEL% NEQ 0 goto :error

echo.
echo Running end-to-end tests...
go test -v . -run TestEndToEnd
if %ERRORLEVEL% NEQ 0 goto :error

echo.
echo Running InfluxDB compatibility tests...
go test -v . -run TestInfluxDBCompatibility
if %ERRORLEVEL% NEQ 0 goto :error

echo.
echo Running load tests...
go test -v . -run "TestConcurrent|TestQuery" -timeout 5m
if %ERRORLEVEL% NEQ 0 goto :error

echo.
echo All tests completed successfully!
exit /b 0

:error
echo.
echo Some tests failed. Please check the output above for details.
exit /b 1 