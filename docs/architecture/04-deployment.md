# Arquitectura — Despliegue

Topología confirmada para V1.

Nota: el flujo localhost de desarrollo/producción local ya no usa el frontend
Next.js. `zv-orchestrator` sirve el workbench HTMX en `http://127.0.0.1:8080/`
y `scripts/run-local.sh` arranca ese entorno sin Node ni TypeScript. La
topología VPS de este documento conserva el esquema histórico de despliegue web.

## Topología

```
┌─────────────────────────────────────────┐         ┌──────────────────────────────────┐
│              VPS (Linux)                │         │           PC del usuario          │
│           — propiedad del user —        │         │             (Windows)             │
│                                         │         │                                  │
│  ┌────────────────────────────────────┐ │         │  ┌────────────────────────────┐  │
│  │ Nginx (TLS, reverse proxy)         │ │         │  │  Recording Driver (Go)     │  │
│  └────────────┬───────────────────────┘ │         │  │   - lanza CS2 + HLAE       │  │
│               │                         │         │  │   - sube clips a MinIO     │  │
│  ┌────────────▼─────────┐  ┌─────────┐  │         │  │   - reporta progreso       │  │
│  │ Frontend (Next.js)   │  │ Orches- │  │ Tailscale│ │  + CS2.exe + HLAE.exe + GPU│  │
│  │  systemd: web        │  │ trator  │◄─────────────┤                            │  │
│  └──────────────────────┘  │ (Go)    │  │  100.x.y │ │  conecta solo cuando el   │  │
│                            │ chi+sqlx│  │          │ │  PC está encendido         │  │
│                            │ +asynq  │  │         │  └────────────────────────────┘  │
│                            └────┬────┘  │         │                                  │
│  ┌──────────────────────┐       │       │         └──────────────────────────────────┘
│  │ Demo Parser (Go)     │◄──────┤       │
│  │  systemd: parser     │       │       │
│  └──────────────────────┘       │       │
│  ┌──────────────────────┐       │       │
│  │ Composer / Mixer /   │◄──────┤       │
│  │ Encoder (Python)     │       │       │
│  │  systemd: media-*    │       │       │
│  └──────────────────────┘       │       │
│                                 │       │
│  ┌──────────┐ ┌─────────┐ ┌─────▼────┐  │
│  │PostgreSQL│ │ Redis   │ │ MinIO    │  │
│  └──────────┘ └─────────┘ └──────────┘  │
└─────────────────────────────────────────┘
```

## Lo que corre dónde

### VPS Linux

| Servicio        | Responsable                        | Puerto      | systemd unit          |
|-----------------|------------------------------------|-------------|------------------------|
| nginx           | TLS termination + reverse proxy    | 80, 443     | `nginx.service`        |
| frontend        | Next.js 15 (Node)                  | 3000 local  | `zackvideo-web.service`|
| orquestador     | API HTTP + WebSocket + job manager | 8080 local  | `zackvideo-api.service`|
| demo parser     | worker que consume jobs Asynq      | (sin port)  | `zackvideo-parser.service` |
| composer        | worker que consume jobs Asynq      | (sin port)  | `zackvideo-composer.service` |
| mixer           | worker que consume jobs Asynq      | (sin port)  | `zackvideo-mixer.service` |
| encoder         | worker que consume jobs Asynq      | (sin port)  | `zackvideo-encoder.service` |
| postgres        | DB                                 | 5432 local  | `postgresql.service`   |
| redis           | cola Asynq + cache                 | 6379 local  | `redis-server.service` |
| minio           | object storage S3-compatible       | 9000 / 9001 | `minio.service`        |
| tailscaled      | malla con el PC del usuario        | (UDP)       | `tailscaled.service`   |

Nginx hace:
- `/` → frontend (Next.js)
- `/api/*` → orquestador
- `/ws/*` → orquestador (WebSocket upgrade)
- `/storage/*` → MinIO (presigned URLs ya van firmadas, así que no se expone el endpoint de admin)

### PC del usuario (Windows)

| Servicio              | Responsable                                  |
|-----------------------|----------------------------------------------|
| Recording Driver      | binario Go que se lanza al boot, conecta al orquestador por Tailscale, hace long-poll de jobs `record` |
| CS2 + HLAE            | invocados por el Recording Driver según necesidad |
| Tailscale             | cliente, vinculado a la misma tailnet del VPS |

El Recording Driver corre como tarea programada al login (o como servicio si se quiere). Si el PC está apagado, los jobs `record` quedan en la cola y se procesan cuando el PC vuelve.

## Conectividad VPS ↔ Worker

**Tailscale.** Razones:
- 0-config NAT traversal. El PC del usuario detrás de un router doméstico puede conectar al VPS sin abrir puertos.
- Identidad por nodo (auth keys), no contraseñas.
- Puede convivir con MagicDNS para que el orquestador apunte a `worker-pc.tailnet.ts.net` en vez de IPs.

Alternativa: Wireguard a mano. Funciona igual de bien, solo más config.

**Direcciones de uso:**
- El orquestador (VPS) NO conoce la IP pública del PC; el PC inicia la conexión Tailscale al arrancar y queda alcanzable como `worker-1.tailnet.ts.net`.
- El Recording Driver (PC) sí conoce la URL del orquestador (`http://orchestrator.tailnet.ts.net:8080` o el dominio público con TLS).

## Despliegue / actualización

Inicialmente, build local + scp + systemd reload. Más adelante, GitHub Actions que cross-compila y publica releases firmados.

```bash
# build local en linux/amd64
make build-orchestrator      # → bin/orchestrator
make build-parser            # → bin/parser

# subir y reiniciar
scp bin/orchestrator vps:/usr/local/bin/zackvideo-orchestrator.new
ssh vps "sudo mv /usr/local/bin/zackvideo-orchestrator.new /usr/local/bin/zackvideo-orchestrator \
         && sudo systemctl restart zackvideo-api"
```

Para el Recording Driver (Windows):
```bash
GOOS=windows GOARCH=amd64 go build -o bin/recorder.exe ./cmd/recorder
# subir a una ruta accesible y notificar al usuario, o auto-update con un endpoint del orquestador
```

## Configuración / secretos

`/etc/zackvideo/config.toml` en el VPS, modo 600, owner del usuario de los servicios. Contiene:
- `database_url`
- `redis_url`
- `minio.endpoint / access_key / secret_key`
- `tailscale.auth_key` (para inscribir el PC sin browser)
- `jwt.secret` (sesiones del frontend)

Nunca commitear este archivo. Hay un `config.example.toml` en el repo con placeholders.

Para el Recording Driver (Windows):
- `%APPDATA%\zackvideo\config.toml` con `orchestrator_url`, `worker_token`, `cs2_path`, `hlae_path`, `recordings_dir`.

## Limitaciones conocidas (V1)

1. **Disponibilidad atada al PC del usuario.** Si el PC está apagado, no hay grabación. Los jobs persisten y se procesan al reconectar — el frontend muestra "esperando worker disponible".
2. **Una sola GPU = throughput de 1 job de grabación a la vez.** Aceptado para V1. El paralelismo de parsing/composición/encoding no tiene este límite (corren en el VPS).
3. **MinIO en el mismo VPS.** Si el VPS tiene poco disco, esto se llena rápido (raw clips ocupan GBs). Plan: política de retención (borrar raws > 7 días, mantener solo finals) y, si hace falta, mover a R2 o B2.
4. **Backup de Postgres.** Snapshot diario con `pg_dump` a un bucket externo (R2 / B2). Definir cuando lleguemos a producción.
5. **Sin observabilidad real.** V1 corre con logs a journald y nada más. Si crece, agregar Loki + Grafana en el mismo VPS.
