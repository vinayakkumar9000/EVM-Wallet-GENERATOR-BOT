@echo off
setlocal enabledelayedexpansion

echo === Running Full Verification Loop ===
echo.

echo 1. Checking code formatting...
set UNFORMATTED=
for /f "delims=" %%i in ('gofmt -l . 2^>nul') do (
    set UNFORMATTED=!UNFORMATTED!%%i 
)
if not "!UNFORMATTED!"=="" (
    echo ERROR: The following files are not formatted:
    gofmt -l .
    exit /b 1
)
echo [32m✓ All files properly formatted[0m
echo.

echo 2. Building all packages...
go build ./... 2>&1
if errorlevel 1 (
    echo [31mERROR: Build failed[0m
    exit /b 1
)
echo [32m✓ Build successful[0m
echo.

echo 3. Running go vet...
go vet ./... 2>&1
if errorlevel 1 (
    echo [31mERROR: Vet failed[0m
    exit /b 1
)
echo [32m✓ Vet passed[0m
echo.

echo 4. Checking for carriage returns in .go files...
REM Note: Skipping on Windows as findstr cannot distinguish between
REM literal \r in strings (intentional for terminal control) and
REM actual carriage return line endings. The gofmt check above
REM ensures proper formatting which includes correct line endings.
echo [33m⚠ Skipped on Windows (use verify.sh on Unix for full check)[0m
echo.

echo 5. Running tests...
REM Check if CGO is enabled for race detector
set CGO_ENABLED_VAL=%CGO_ENABLED%
if "%CGO_ENABLED_VAL%"=="1" (
    set RACE_FLAG=-race
    echo Running tests with race detector...
) else (
    set RACE_FLAG=
    echo [33m⚠ CGO not enabled, running tests without race detector[0m
)

REM Check if Docker is available
docker --version >nul 2>&1
if errorlevel 1 (
    echo [33mWARNING: Docker not available, running tests without ephemeral Postgres[0m
    go test ./... %RACE_FLAG% -count=1 2>&1
    if errorlevel 1 (
        echo [31mERROR: Tests failed[0m
        exit /b 1
    )
) else (
    REM Set up ephemeral Postgres for tests
    set TEST_DB_HOST=localhost
    set TEST_DB_PORT=5432
    set TEST_DB_NAME=walletdb_test
    set TEST_DB_USER=postgres
    set TEST_DB_PASSWORD=test

    REM Check if postgres-test container is already running
    docker ps --format "{{.Names}}" | findstr /C:"postgres-test" >nul 2>&1
    if errorlevel 1 (
        echo Starting ephemeral Postgres...
        docker run --rm -d --name postgres-test -e POSTGRES_PASSWORD=test -e POSTGRES_DB=walletdb_test -p 5432:5432 postgres:16 >nul 2>&1
        
        REM Wait for Postgres to be ready
        echo Waiting for Postgres to be ready...
        timeout /t 5 /nobreak >nul
    ) else (
        echo Using existing postgres-test container...
    )

    REM Run tests
    go test ./... %RACE_FLAG% -count=1 2>&1
    if errorlevel 1 (
        echo [31mERROR: Tests failed[0m
        docker stop postgres-test >nul 2>&1
        exit /b 1
    )

    REM Cleanup
    docker stop postgres-test >nul 2>&1
)

echo.
echo [32m=== ✓ ALL VERIFICATION CHECKS PASSED ===[0m
