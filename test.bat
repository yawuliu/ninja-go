@echo off
chcp 65001
REM ============================================================
REM  Ninja-go Test Script
REM  Runs cmake-examples using ninja-go via mingw32-make
REM
REM  Usage:
REM    test.bat                  Run all basic tests (default)
REM    test.bat hello-cmake      Run a single example
REM    test.bat test-basic       Run 01-basic examples
REM    test.bat test-all         Run all examples
REM    test.bat clean            Clean build artifacts
REM    test.bat help             Show this help
REM ============================================================
setlocal enabledelayedexpansion

REM --- Tool paths (from CLAUDE.md) ---
set MAKE=D:/soft/TDM-GCC-64/bin/mingw32-make.exe
set CMAKE=F:/dev8/cmake-3.27.6-windows-x86_64/bin/cmake.exe
set GCC=D:/soft/TDM-GCC-64/bin/gcc.exe
set GXX=D:/soft/TDM-GCC-64/bin/g++.exe

REM --- Change to project root ---
cd /d "%~dp0"

REM --- Parse argument ---
set TARGET=%~1
if "%TARGET%"=="" set TARGET=test-basic

REM --- Help ---
if /i "%TARGET%"=="help" goto :help
if /i "%TARGET%"=="-h" goto :help
if /i "%TARGET%"=="--help" goto :help
if /i "%TARGET%"=="/?" goto :help

REM --- Run ---
echo ============================================================
echo  Ninja-go Test Runner
echo ============================================================
echo  Target: %TARGET%
echo ============================================================
echo.

"%MAKE%" %TARGET% CMAKE="%CMAKE%" GCC="%GCC%" GXX="%GXX%"

if %ERRORLEVEL% neq 0 (
    echo.
    echo ============================================================
    echo  FAILED (exit code: %ERRORLEVEL%)
    echo ============================================================
    exit /b %ERRORLEVEL%
)

echo.
echo ============================================================
echo  PASSED
echo ============================================================
exit /b 0

:help
echo Ninja-go Test Script
echo =====================
echo.
echo Usage: test.bat [target]
echo.
echo Targets:
echo   (none^)              Run 01-basic examples (default^)
echo   hello-cmake          A-hello-cmake
echo   hello-headers        B-hello-headers
echo   static-library       C-static-library
echo   shared-library       D-shared-library
echo   installing           E-installing
echo   build-type           F-build-type
echo   compile-flags        G-compile-flags
echo   third-party-library  H-third-party-library
echo   compiling-with-clang I-compiling-with-clang
echo   building-with-ninja  J-building-with-ninja
echo   imported-targets     K-imported-targets
echo   cpp-standard-i       L-cpp-standard/i-common-method
echo   cpp-standard-ii      L-cpp-standard/ii-cxx-standard
echo   cpp-standard-iii     L-cpp-standard/iii-compile-features
echo   sub-projects         02-sub-projects/A-basic
echo   configure-files      03-code-generation/configure-files
echo   protobuf             03-code-generation/protobuf
echo   clang-analyzer       04-static-analysis/clang-analyzer
echo   clang-format         04-static-analysis/clang-format
echo   cppcheck             04-static-analysis/cppcheck
echo   boost-test           05-unit-testing/boost
echo   catch2-test          05-unit-testing/catch2-vendored
echo   google-test          05-unit-testing/google-test-download
echo.
echo   test-basic            Run all 01-basic examples
echo   test-sub-projects     Run 02-sub-projects examples
echo   test-code-generation  Run 03-code-generation examples
echo   test-static-analysis  Run 04-static-analysis examples
echo   test-unit-testing     Run 05-unit-testing examples
echo   test-all              Run ALL examples
echo.
echo   clean                 Clean all build artifacts
echo   help                  Show this help
echo.
echo Environment:
echo   MAKE  = %MAKE%
echo   CMAKE = %CMAKE%
echo   GCC   = %GCC%
echo   GXX   = %GXX%
exit /b 0
