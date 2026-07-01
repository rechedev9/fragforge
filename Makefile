.PHONY: build test check skills-check workflows-check up down migrate-up migrate-media-up migrate-down fmt vet

build:
	go build -o bin/zv ./cmd/zv
	go build -o bin/zv-parser ./cmd/zv-parser
	go build -o bin/zv-demo-players ./cmd/zv-demo-players
	go build -o bin/zv-orchestrator ./cmd/zv-orchestrator
	go build -o bin/zv-recorder ./cmd/zv-recorder
	go build -o bin/zv-composer ./cmd/zv-composer
	go build -o bin/zv-editor ./cmd/zv-editor
	go build -o bin/zv-rhythm ./cmd/zv-rhythm
	go build -o bin/zv-analysis-viewer ./cmd/zv-analysis-viewer
	go build -o bin/zv-pipeline ./cmd/zv-pipeline
	go build -o bin/zv-tactical-data ./cmd/zv-tactical-data
	go build -o bin/zv-agent ./cmd/zv-agent

test:
	go test ./... -count=1
	go run ./cmd/zv check

check:
	go run ./cmd/zv check

skills-check:
	go run ./cmd/zv skills check

workflows-check:
	go run ./cmd/zv workflows check

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
