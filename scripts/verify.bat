@echo off
setlocal enabledelayedexpansion

echo === Running verification loop ===
echo.

echo 1. Checking formatting...
gofmt -l . > temp_fmt.txt
set /p UNFORMATTED=<temp_fmt.txt
if not "!UNFORMATTED!"=="" (
    echo ERROR: Unformatted files:
    type temp_fmt.txt
    del temp_fmt.txt
    exit /b 1
)
del temp_fmt.txt
echo ✓ All files formatted
echo.

echo 2. Building...
go build ./...
if errorlevel 1 (
    echo ERROR: Build failed
    exit /b 1
)
echo ✓ Build successful
echo.

echo 3. Running go vet...
go vet ./...
if errorlevel 1 (
    echo ERROR: Vet failed
    exit /b 1
)
echo ✓ Vet passed
echo.

echo 4. Running tests...
go test ./... -count=1
if errorlevel 1 (
    echo ERROR: Tests failed
    exit /b 1
)
echo ✓ All tests passed
echo.

echo.
echo === ✓ ALL CHECKS PASSED ===
echo Note: Race detector requires CGO (not available on Windows by default)
echo       Run on Linux/Mac with: go test ./... -race -count=1
exit /b 0