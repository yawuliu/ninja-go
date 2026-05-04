# Ninja-go Makefile
# Builds the ninja-go binary and uses it to compile cmake-examples.

# --- Tool paths (override via env or cmdline) ---
# cmake 3.27.6: F:/dev8/cmake-3.27.6-windows-x86_64/bin/cmake.exe
CMAKE          ?= F:/dev8/cmake-3.27.6-windows-x86_64/bin/cmake.exe
# Real ninja (C++ build) for cmake generation only
# Must be absolute so cmake can find it from the build directory
NINJA_REAL     ?= $(CURDIR)/ninja.exe
# TDM-GCC: D:/soft/TDM-GCC-64/bin
GCC            ?= D:/soft/TDM-GCC-64/bin/gcc.exe
GXX            ?= D:/soft/TDM-GCC-64/bin/g++.exe

# --- Configuration ---
# Force bash/sh shell so recipes work (mingw32-make defaults to cmd.exe on Windows)
SHELL          := sh
NINJA_GO_BIN   := ninja-go.exe
RM             := rm -f
MKDIR          := mkdir -p
RMDIR          := rm -rf

GO             := go
GO_FLAGS       :=
NINJA_GO_DIR   := ninja
EXAMPLES_DIR   := testdata/cmake-examples
BUILD_BASE_DIR := $(EXAMPLES_DIR)/_build

# Pass compiler to cmake via environment
export CC  := $(GCC)
export CXX := $(GXX)

# Root cmake-examples dir (used for cmake -S/-B)
CMAKE_EXAMPLES := $(abspath $(EXAMPLES_DIR))

# Set the ninja-go binary path (depends on `build-ninja-go`)
NINJA_GO := $(abspath $(NINJA_GO_BIN))

.PHONY: all build-ninja-go test-all test-basic test-sub-projects test-code-generation test-static-analysis test-unit-testing clean help

# --- Default target ---
all: build-ninja-go test-basic

# --- Help ---
help:
	@echo "Ninja-go Makefile"
	@echo ""
	@echo "Usage:"
	@echo "  make build-ninja-go    Build the ninja-go binary"
	@echo "  make test-all           Run all cmake-examples"
	@echo "  make test-basic         Run 01-basic examples"
	@echo "  make test-sub-projects  Run 02-sub-projects examples"
	@echo "  make test-code-generation Run 03-code-generation examples"
	@echo "  make test-static-analysis Run 04-static-analysis examples"
	@echo "  make test-unit-testing   Run 05-unit-testing examples"
	@echo "  make <example-name>     Run a specific example"
	@echo "  make clean              Clean all build artifacts and ninja-go binary"
	@echo "  make help               Show this help"
	@echo ""
	@echo "Available examples:"
	@echo "  hello-cmake, hello-headers, static-library, shared-library"
	@echo "  installing, build-type, compile-flags, third-party-library"
	@echo "  compiling-with-clang, building-with-ninja, imported-targets"
	@echo "  cpp-standard-i, cpp-standard-ii, cpp-standard-iii"
	@echo "  sub-projects, configure-files, protobuf"
	@echo "  clang-analyzer, clang-format, cppcheck, cppcheck-compile-commands"
	@echo "  boost-test, catch2-test, google-test, deb-installer"
	@echo "  conan-i-basic, conan-ii-basic-targets"

# --- Build ninja-go ---
build-ninja-go:
	@echo "Building ninja-go..."
	cd $(NINJA_GO_DIR) && $(GO) build $(GO_FLAGS) -o ../$(NINJA_GO_BIN) .
	@echo "ninja-go built: $(NINJA_GO_BIN)"

# --- Run cmake to generate build.ninja, then build with ninja-go ---
# Usage: $(call run_cmake_ninja,<example-dir>,<target-name>)
define run_cmake_ninja
	@echo "============================================================"
	@echo "Testing: $(2)"
	@echo "============================================================"
	@$(MKDIR) "$(BUILD_BASE_DIR)/$(2)"
	@cd "$(BUILD_BASE_DIR)/$(2)" && \
		$(CMAKE) -G "Ninja" \
			-DCMAKE_MAKE_PROGRAM="$(NINJA_REAL)" \
			-DCMAKE_C_COMPILER="$(GCC)" \
			-DCMAKE_CXX_COMPILER="$(GXX)" \
			"$(CMAKE_EXAMPLES)/$(1)" && \
		echo "--- Running ninja-go ---" && \
		"$(NINJA_GO)" $(NINJA_FLAGS) || \
		(echo "CMake generation or build failed for $(2)" && exit 1)
	@echo ""
endef

# --- Individual example targets ---
hello-cmake: build-ninja-go
	$(call run_cmake_ninja,01-basic/A-hello-cmake,hello-cmake)

hello-headers: build-ninja-go
	$(call run_cmake_ninja,01-basic/B-hello-headers,hello-headers)

static-library: build-ninja-go
	$(call run_cmake_ninja,01-basic/C-static-library,static-library)

shared-library: build-ninja-go
	$(call run_cmake_ninja,01-basic/D-shared-library,shared-library)

installing: build-ninja-go
	$(call run_cmake_ninja,01-basic/E-installing,installing)

build-type: build-ninja-go
	$(call run_cmake_ninja,01-basic/F-build-type,build-type)

compile-flags: build-ninja-go
	$(call run_cmake_ninja,01-basic/G-compile-flags,compile-flags)

third-party-library: build-ninja-go
	$(call run_cmake_ninja,01-basic/H-third-party-library,third-party-library)

compiling-with-clang: build-ninja-go
	$(call run_cmake_ninja,01-basic/I-compiling-with-clang,compiling-with-clang)

building-with-ninja: build-ninja-go
	$(call run_cmake_ninja,01-basic/J-building-with-ninja,building-with-ninja)

imported-targets: build-ninja-go
	$(call run_cmake_ninja,01-basic/K-imported-targets,imported-targets)

cpp-standard-i: build-ninja-go
	$(call run_cmake_ninja,01-basic/L-cpp-standard/i-common-method,cpp-standard-i)

cpp-standard-ii: build-ninja-go
	$(call run_cmake_ninja,01-basic/L-cpp-standard/ii-cxx-standard,cpp-standard-ii)

cpp-standard-iii: build-ninja-go
	$(call run_cmake_ninja,01-basic/L-cpp-standard/iii-compile-features,cpp-standard-iii)

sub-projects: build-ninja-go
	$(call run_cmake_ninja,02-sub-projects/A-basic,sub-projects)

configure-files: build-ninja-go
	$(call run_cmake_ninja,03-code-generation/configure-files,configure-files)

protobuf: build-ninja-go
	$(call run_cmake_ninja,03-code-generation/protobuf,protobuf)

clang-analyzer: build-ninja-go
	$(call run_cmake_ninja,04-static-analysis/clang-analyzer,clang-analyzer)

clang-format: build-ninja-go
	$(call run_cmake_ninja,04-static-analysis/clang-format,clang-format)

cppcheck: build-ninja-go
	$(call run_cmake_ninja,04-static-analysis/cppcheck,cppcheck)

cppcheck-compile-commands: build-ninja-go
	$(call run_cmake_ninja,04-static-analysis/cppcheck-compile-commands,cppcheck-compile-commands)

boost-test: build-ninja-go
	$(call run_cmake_ninja,05-unit-testing/boost,boost-test)

catch2-test: build-ninja-go
	$(call run_cmake_ninja,05-unit-testing/catch2-vendored,catch2-test)

google-test: build-ninja-go
	$(call run_cmake_ninja,05-unit-testing/google-test-download,google-test)

deb-installer: build-ninja-go
	$(call run_cmake_ninja,06-installer/deb,deb-installer)

conan-i-basic: build-ninja-go
	$(call run_cmake_ninja,07-package-management/D-conan/i-basic,conan-i-basic)

conan-ii-basic-targets: build-ninja-go
	$(call run_cmake_ninja,07-package-management/D-conan/ii-basic-targets,conan-ii-basic-targets)

# --- Group targets ---
# 01-basic examples
test-basic: hello-cmake hello-headers static-library shared-library \
	installing build-type compile-flags third-party-library \
	compiling-with-clang building-with-ninja imported-targets \
	cpp-standard-i cpp-standard-ii cpp-standard-iii

# 02-sub-projects examples
test-sub-projects: sub-projects

# 03-code-generation examples
test-code-generation: configure-files protobuf

# 04-static-analysis examples
test-static-analysis: clang-analyzer clang-format cppcheck cppcheck-compile-commands

# 05-unit-testing examples
test-unit-testing: boost-test catch2-test google-test

# 06-installer examples
test-installer: deb-installer

# 07-package-management examples
test-package-management: conan-i-basic conan-ii-basic-targets

# Run ALL examples
test-all: test-basic test-sub-projects test-code-generation \
	test-static-analysis test-unit-testing test-installer test-package-management

# --- Clean ---
clean:
	@echo "Cleaning build artifacts..."
	-$(RM) "$(NINJA_GO_BIN)"
	-$(RMDIR) "$(BUILD_BASE_DIR)"
	@echo "Clean complete."
