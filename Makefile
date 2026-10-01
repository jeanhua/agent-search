.PHONY: build run test vet docker docker-up docker-down

build:
	go build -trimpath -ldflags="-s -w" -o agent-search ./cmd/agent-search

run:
	go run ./cmd/agent-search

test:
	go test ./...

vet:
	go vet ./...

docker:
	docker compose up -d --build

docker-up:
	docker compose up -d

docker-down:
	docker compose down
