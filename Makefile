.PHONY: build test up down migrate-up migrate-down fmt vet

build:
	go build -o bin/zv-parser ./cmd/zv-parser
	go build -o bin/zv-orchestrator ./cmd/zv-orchestrator

test:
	go test ./... -count=1

up:
	docker compose up -d

down:
	docker compose down

migrate-up:
	@psql "$$ZV_DATABASE_URL" -f migrations/001_jobs.up.sql

migrate-down:
	@psql "$$ZV_DATABASE_URL" -f migrations/001_jobs.down.sql

fmt:
	gofmt -w .

vet:
	go vet ./...
