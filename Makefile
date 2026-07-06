.PHONY: build run dev clean test

build:
	go build -o ./bin/api ./cmd/api

run:
	go run ./cmd/api

dev:
	DEV=true DB_ENCRYPTION_KEY=$$(cat .env | grep DB_ENCRYPTION_KEY | cut -d= -f2) go run ./cmd/api

test:
	go test -race ./...

test-coverage:
	go test -race -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

lint:
	golangci-lint run ./...

clean:
	rm -rf ./bin ./data

docker-build:
	docker compose build

docker-up:
	docker compose up -d

docker-down:
	docker compose down

docker-logs:
	docker compose logs -f
