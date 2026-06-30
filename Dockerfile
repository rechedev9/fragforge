# Orchestrator image: builds the zv-orchestrator binary and runs it.
# Capture (HLAE/CS2) is a Windows+GPU stage and is NOT available in this Linux
# container; this image serves the API and the in-process parse/scan workers.
# syntax=docker/dockerfile:1

FROM golang:1.26 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# Pure-Go static binary, so the runtime can be distroless/static.
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/zv-orchestrator ./cmd/zv-orchestrator

FROM gcr.io/distroless/static-debian12
COPY --from=build /out/zv-orchestrator /usr/local/bin/zv-orchestrator
# Local-first defaults: in-memory repo + inline queue (no Postgres/Redis), and a
# 0.0.0.0 bind so the web container can reach it. A non-loopback bind requires
# ZV_MUTATION_TOKEN (set by compose); record/render workers stay off (no tool
# paths), which capture readiness reports as "set up capture".
ENV ZV_DATABASE_URL=memory \
    ZV_HTTP_ADDR=0.0.0.0:8080 \
    ZV_DATA_DIR=/data
EXPOSE 8080
VOLUME ["/data"]
ENTRYPOINT ["/usr/local/bin/zv-orchestrator"]
