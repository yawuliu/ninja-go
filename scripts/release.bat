@echo off
REM ============================================================
REM  ninja-go Release Script (Windows)
REM  Cross-compile and create a GitHub release
REM
REM  Usage:
REM    scripts\release.bat v0.1.0
REM
REM  Requires: Go, git, gh (GitHub CLI)
REM ============================================================
setlocal enabledelayedexpansion

set VERSION=%~1
if "%VERSION%"=="" (
    echo Usage: %~nx0 ^<version^>
    echo Example: %~nx0 v0.1.0
    exit /b 1
)

cd /d "%~dp0.."

set BUILD_DIR=%cd%\_release
if exist "%BUILD_DIR%" rmdir /s /q "%BUILD_DIR%"
mkdir "%BUILD_DIR%"

REM ---- Check prerequisites ----
where go  >nul 2>&1 || (echo ERROR: go not found && exit /b 1)
where git >nul 2>&1 || (echo ERROR: git not found && exit /b 1)
where gh  >nul 2>&1 || (
    echo ERROR: gh (GitHub CLI^) not found
    echo Install: https://cli.github.com/
    exit /b 1
)

REM ---- Verify working tree is clean ----
git diff-index --quiet HEAD -- 2>nul || (
    echo ERROR: working tree is not clean. Please commit or stash changes.
    exit /b 1
)

REM ---- Cross-compile ----
echo === Building %VERSION% ===

set LDFLAGS=-s -w

REM --- Windows amd64 ---
set WIN_BIN=ninja-go-%VERSION%-windows-amd64.exe
echo   -^> %WIN_BIN% (GOOS=windows GOARCH=amd64)
set GOOS=windows
set GOARCH=amd64
go build -ldflags="%LDFLAGS%" -o "%BUILD_DIR%\%WIN_BIN%" .\ninja\
if not exist "%BUILD_DIR%\%WIN_BIN%" (
    echo ERROR: Windows build failed
    exit /b 1
)

REM --- Linux amd64 ---
set LINUX_BIN=ninja-go-%VERSION%-linux-amd64
echo   -^> %LINUX_BIN% (GOOS=linux GOARCH=amd64)
set GOOS=linux
set GOARCH=amd64
go build -ldflags="%LDFLAGS%" -o "%BUILD_DIR%\%LINUX_BIN%" .\ninja\
if not exist "%BUILD_DIR%\%LINUX_BIN%" (
    echo ERROR: Linux build failed
    exit /b 1
)

echo.
echo Binaries:
for %%f in ("%BUILD_DIR%\*") do echo   %%~nxf   %%~zf bytes

REM ---- Tag ----
echo.
echo === Creating tag %VERSION% ===
git tag -a "%VERSION%" -m "ninja-go %VERSION%"
git push origin "%VERSION%"

REM ---- Create GitHub release ----
echo.
echo === Creating GitHub release ===
gh release create "%VERSION%" ^
    --title "ninja-go %VERSION%" ^
    --notes "Go port of Ninja build system. %VERSION%.
Downloads:
- ninja-go-%VERSION%-windows-amd64.exe
- ninja-go-%VERSION%-linux-amd64" ^
    "%BUILD_DIR%\%WIN_BIN%" ^
    "%BUILD_DIR%\%LINUX_BIN%"

REM ---- Cleanup ----
rmdir /s /q "%BUILD_DIR%" 2>nul

echo.
echo === Release %VERSION% created ===
exit /b 0
