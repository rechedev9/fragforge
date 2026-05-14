.PHONY: build test up down migrate-up migrate-down fmt vet

# zv-orchestrator will be added to `build` once cmd/zv-orchestrator exists (later task).
build:
	go build -o bin/zv-parser ./cmd/zv-parser

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
