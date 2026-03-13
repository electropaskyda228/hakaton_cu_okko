.PHONY: run build deps docker-up docker-down

run:
	go run cmd/api/main.go

build:
	go build -o bin/scene-detector cmd/api/main.go

deps:
	go mod download
	go mod tidy

docker-up:
	docker-compose up -d

docker-down:
	docker-compose down

test:
	go test ./...