.PHONY: all test build clean test-go test-python build-go build-python compare-implementations demo demo-go demo-python scripts-permissions install install-go install-python demo-aws-cli s3-go s3-python s3-aws-cli

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

# AWS CLI settings
AWS_CLI := aws
AWS_CLI_VERSION := $(shell aws --version 2>/dev/null)

all: test build

# Combined commands
test: test-go test-python test-aws-cli
	${INFO} All tests completed successfully

build: build-go build-python build-aws-cli
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
demo: demo-go demo-python demo-aws-cli
	${INFO} All demos completed

demo-go: build-go scripts-permissions
	${INFO} Running Go demo...
	@./scripts/demo.sh go

demo-python: build-python scripts-permissions
	${INFO} Running Python demo...
	@./scripts/demo.sh python

demo-aws-cli: build-aws-cli scripts-permissions
	${INFO} Running AWS CLI demo...
	@./scripts/demo.sh aws-cli

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

# AWS CLI specific commands
check-aws-cli:
	@if ! command -v ${AWS_CLI} >/dev/null 2>&1; then \
		echo "AWS CLI is not installed."; \
		read -p "Would you like to install AWS CLI? (y/n) " answer; \
		if [ "$$answer" = "y" ]; then \
			case "$$(uname -s)" in \
				Linux*) \
					curl "https://awscli.amazonaws.com/awscli-exe-linux-x86_64.zip" -o "awscliv2.zip" && \
					unzip awscliv2.zip && \
					sudo ./aws/install && \
					rm -rf aws awscliv2.zip ;; \
				Darwin*) \
					brew install awscli ;; \
				*) \
					echo "Please install AWS CLI manually: https://aws.amazon.com/cli/" ;; \
			esac \
		else \
			echo "AWS CLI is required. Please install it manually: https://aws.amazon.com/cli/"; \
			exit 1; \
		fi \
	else \
		echo "AWS CLI is installed: ${AWS_CLI_VERSION}"; \
	fi

test-aws-cli: check-aws-cli
	${INFO} Running AWS CLI tests...
	cd src/aws-cli && ./integrity.sh \
		--bucket ${DEMO_BUCKET} \
		--text ${DEMO_TEXT} \
		--key ${DEMO_KEY} \
		--endpoint-url ${DEMO_ENDPOINT} \
		--profile ${DEMO_PROFILE} \
		--region ${DEMO_REGION} \
		--verbose

build-aws-cli: check-aws-cli
	${INFO} Setting up AWS CLI script...
	chmod +x src/aws-cli/integrity.sh

# S3 settings (referencing existing demo settings)
S3_BUCKET := crc32
S3_TEXT := "Hello, World"
S3_KEY := test.txt
S3_ENDPOINT := ${S3_ENDPOINT}
S3_PROFILE := default
S3_REGION := auto

# Direct SDK commands
s3-go: build-go
	@echo "Running Go S3 implementation..."
	./bin/integrity \
		-bucket ${S3_BUCKET} \
		-text ${S3_TEXT} \
		-key ${S3_KEY} \
		-endpoint-url ${S3_ENDPOINT} \
		-profile ${S3_PROFILE} \
		-region ${S3_REGION} \
		-verbose

s3-python: build-python
	@echo "Running Python S3 implementation..."
	python src/python/integrity.py \
		--bucket ${S3_BUCKET} \
		--text ${S3_TEXT} \
		--key ${S3_KEY} \
		--endpoint-url ${S3_ENDPOINT} \
		--profile ${S3_PROFILE} \
		--region ${S3_REGION} \
		--verbose

s3-aws-cli: build-aws-cli
	@echo "Running AWS CLI S3 implementation..."
	./src/aws-cli/integrity.sh \
		--bucket ${S3_BUCKET} \
		--text ${S3_TEXT} \
		--key ${S3_KEY} \
		--endpoint-url ${S3_ENDPOINT} \
		--profile ${S3_PROFILE} \
		--region ${S3_REGION} \
		--verbose


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