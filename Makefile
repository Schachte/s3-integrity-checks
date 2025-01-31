.PHONY: all test build clean test-go test-python build-go build-python compare-implementations demo demo-go demo-python scripts-permissions install install-go install-python

# Default Python and Go paths
PYTHON_SRC := src/python
GO_SRC := src/go
GO_CMD := src/go/cmd

# Python settings
PYTHON := python3
PYTEST := pytest
PIP := pip

# Go settings
GO := go
GO_BUILD_OUTPUT := bin/integrity

# Colors for pretty output
GREEN := \033[0;32m
NC := \033[0m # No Color
INFO := @echo "${GREEN}=>${NC}"

# Demo settings
DEMO_BUCKET := crc32
DEMO_TEXT := "Hello, World"
DEMO_KEY := test.txt
DEMO_ENDPOINT := https://https://<S3 ENDPOINT>
DEMO_PROFILE := default
DEMO_REGION := auto

all: test build

# Combined commands
test: test-go test-python
	${INFO} All tests completed successfully

build: build-go build-python
	${INFO} All builds completed successfully

# Go specific commands
test-go:
	${INFO} Running Go tests...
	cd ${GO_SRC} && ${GO} test -v ./...

build-go:
	${INFO} Building Go binary...
	mkdir -p bin
	${GO} build -o ${GO_BUILD_OUTPUT} ${GO_CMD}/main.go

# Python specific commands
test-python:
	${INFO} Running Python tests...
	${PYTEST} ${PYTHON_SRC}/tests/

build-python:
	${INFO} Installing Python dependencies...
	${PIP} install -r ${PYTHON_SRC}/requirements.txt

# Cleaning
clean:
	${INFO} Cleaning build artifacts...
	rm -rf bin/
	find . -type d -name "__pycache__" -exec rm -rf {} +
	find . -type f -name "*.pyc" -delete
	find . -type f -name ".coverage" -delete
	find . -type d -name ".pytest_cache" -exec rm -rf {} +
	${INFO} Clean completed

# Comparison
compare-implementations: build
	${INFO} Comparing implementations...
	@chmod +x scripts/compare_implementations.sh
	@./scripts/compare_implementations.sh

# Demo commands
demo: demo-go demo-python
	${INFO} Both demos completed

demo-go: build-go scripts-permissions
	${INFO} Running Go demo...
	@./scripts/demo.sh go

demo-python: build-python scripts-permissions
	${INFO} Running Python demo...
	@./scripts/demo.sh python

# Add new target for script permissions
scripts-permissions:
	${INFO} Setting script permissions...
	@chmod +x scripts/*.sh

# Installation
install: install-go install-python
	${INFO} Installation completed

install-go: 
	${INFO} Installing Go dependencies...
	cd ${GO_SRC} && go mod tidy
	${INFO} Building Go binary...
	make build-go

install-python:
	${INFO} Installing Python dependencies...
	python -m pip install --upgrade pip
	pip install -r ${PYTHON_SRC}/requirements.txt

# Help
help:
	@echo "Available commands:"
	@echo "  make install      - Install all dependencies and build binaries"
	@echo "  make install-go   - Install Go dependencies only"
	@echo "  make install-python - Install Python dependencies only"
	@echo "  make all          - Run all tests and builds"
	@echo "  make test         - Run all tests (Go and Python)"
	@echo "  make build        - Build all components"
	@echo "  make test-go      - Run Go tests only"
	@echo "  make test-python  - Run Python tests only"
	@echo "  make build-go     - Build Go binary only"
	@echo "  make build-python - Install Python dependencies only"
	@echo "  make clean        - Clean build artifacts"
	@echo "  make demo         - Run both Go and Python demos"
	@echo "  make demo-go      - Run Go demo only"
	@echo "  make demo-python  - Run Python demo only"
	@echo "  make compare-implementations - Run both implementations and save outputs"