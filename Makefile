.PHONY: build test up down migrate-up migrate-media-up migrate-down fmt vet

build:
	go build -o bin/zv-parser ./cmd/zv-parser
	go build -o bin/zv-demo-players ./cmd/zv-demo-players
	go build -o bin/zv-orchestrator ./cmd/zv-orchestrator
	go build -o bin/zv-recorder ./cmd/zv-recorder
	go build -o bin/zv-composer ./cmd/zv-composer
	go build -o bin/zv-editor ./cmd/zv-editor
	go build -o bin/zv-pipeline ./cmd/zv-pipeline

test:
	go test ./... -count=1

up:
	docker compose up -d

down:
	docker compose down

migrate-up:
	@psql "$$ZV_DATABASE_URL" -f migrations/001_jobs.up.sql
	@psql "$$ZV_DATABASE_URL" -f migrations/002_job_status_media.up.sql

migrate-media-up:
	@psql "$$ZV_DATABASE_URL" -f migrations/002_job_status_media.up.sql

migrate-down:
	@psql "$$ZV_DATABASE_URL" -f migrations/002_job_status_media.down.sql
	@psql "$$ZV_DATABASE_URL" -f migrations/001_jobs.down.sql

fmt:
	gofmt -w .

vet:
	go vet ./...
