.PHONY: all build test clean generate-grpc run-game run-match docker-build docker-up docker-down

all: build

generate-grpc:
	@echo "Generating gRPC code..."
	@pwsh -ExecutionPolicy Bypass -File ./generate_grpc.ps1

deps:
	@echo "Downloading dependencies..."
	@go mod download
	@go mod tidy

build: deps generate-grpc
	@echo "Building game server..."
	@go build -o bin/game-server ./cmd/game-server
	@echo "Building match server..."
	@go build -o bin/match-server ./cmd/match-server

build-linux: deps generate-grpc
	@echo "Building game server (Linux)..."
	@GOOS=linux GOARCH=amd64 go build -o bin/game-server-linux ./cmd/game-server
	@echo "Building match server (Linux)..."
	@GOOS=linux GOARCH=amd64 go build -o bin/match-server-linux ./cmd/match-server

test:
	@echo "Running tests..."
	@go test -v ./internal/...

test-coverage:
	@echo "Running tests with coverage..."
	@go test -coverprofile=coverage.out ./internal/...
	@go tool cover -html=coverage.out -o coverage.html

run-game: build
	@echo "Running game server..."
	@./bin/game-server

run-match: build
	@echo "Running match server..."
	@./bin/match-server

docker-build:
	@echo "Building Docker images..."
	@docker-compose build

docker-up:
	@echo "Starting Docker containers..."
	@docker-compose up -d

docker-down:
	@echo "Stopping Docker containers..."
	@docker-compose down

docker-logs:
	@docker-compose logs -f

clean:
	@echo "Cleaning..."
	@rm -rf bin/
	@rm -f coverage.out coverage.html
