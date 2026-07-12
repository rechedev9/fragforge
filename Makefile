.PHONY: build test check skills-check workflows-check fmt vet

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
	go build -o bin/zv-tactical-data ./cmd/zv-tactical-data
	go build -o bin/zv-tui ./cmd/zv-tui

test:
	go test ./... -count=1
	go run ./cmd/zv check

check:
	go run ./cmd/zv check

skills-check:
	go run ./cmd/zv skills check

workflows-check:
	go run ./cmd/zv workflows check

fmt:
	gofmt -w .

vet:
	go vet ./...
