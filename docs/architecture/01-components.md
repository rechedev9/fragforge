# Arquitectura — Componentes

Cada componente es un proceso (o conjunto de procesos) con una responsabilidad clara y una interfaz definida. Pueden vivir en máquinas separadas o en la misma; el contrato es por API / mensajes.

## 1. Frontend (Web app)

**Responsabilidad:** UI para subir demos, elegir jugador objetivo, elegir preset visual, ver progreso, descargar / compartir resultado.

**Interfaz:**
- `POST /jobs` (multipart: `.dem` + JSON config).
- `GET /jobs/{id}` (estado + URLs de artefactos).
- WebSocket `/jobs/{id}/events` para progreso en vivo.

**Stack candidato:** Next.js + Tailwind (rápido para iterar). Decisión final en [`03-stack-decisions.md`](./03-stack-decisions.md).

## 2. Orquestador (API + Job Manager)

**Responsabilidad:**
- Validar input (archivo válido, jugador presente en la demo, config sana).
- Crear el job, persistir en DB, encolar.
- Asignar tareas a cada worker (parser, recording, composer, mixer, encoder).
- Persistir estado y artefactos intermedios.
- Servir API al frontend.

**Estado persistido:** PostgreSQL para metadata, S3-compatible object storage (R2 / MinIO) para blobs (demos, raw clips, outputs).

**Cola:** Redis + un broker simple (Asynq si vamos en Go, ARQ si vamos en Python) o NATS si queremos algo más serio.

## 3. Demo Parser Service (Go)

**Responsabilidad:** dado un `.dem`, emitir el "kill plan" — lista estructurada de eventos relevantes para el jugador objetivo.

**Input:**
```json
{
  "demo_path": "s3://bucket/abc.dem",
  "target_steamid64": "76561198...",
  "rules": {
    "min_kills_in_window": 1,
    "window_seconds": 10,
    "weapons": ["awp", "deagle", "usp_silencer", "..."]
  }
}
```

**Output (kill plan):**
```json
{
  "demo_id": "...",
  "tickrate": 64,
  "map": "de_inferno",
  "segments": [
    {
      "id": "seg-001",
      "round": 7,
      "tick_start": 102340,
      "tick_end": 103200,
      "kills": [
        {"tick": 102450, "weapon": "awp", "headshot": true,
         "victim_steamid64": "...", "killer_pos": [x,y,z],
         "victim_pos": [x,y,z]}
      ]
    }
  ]
}
```

**Implementación:** binario Go con [`demoinfocs-golang`](https://github.com/markus-wa/demoinfocs-golang). Usa `RegisterEventHandler(events.Kill)` y agrupa kills consecutivas en ventanas temporales según las reglas.

Ver detalles en [`research/01-demo-parsing.md`](../research/01-demo-parsing.md).

## 4. Recording Driver (Windows worker)

**Responsabilidad:** dado un `kill plan` y el `.dem`, abrir CS2 + HLAE, ir a cada segmento, grabar y devolver clips crudos.

**Sub-componentes:**

- **HLAE Controller:** lanza CS2 con HLAE inyectado y `mirv_script_load` apuntando al script de control. Pasa por CLI:
  - Ruta a `.dem`
  - Ruta al script JavaScript generado para HLAE 2.x
  - Carpeta de salida
- **Script JavaScript generado:** usa el callback de frame de HLAE 2.x para programar comandos a ticks específicos:
  - `demo_gototick <start>`
  - `spec_player_by_accountid <target>` (cámara fija al jugador)
  - `mirv_streams record start` al entrar al rango de la kill
  - `mirv_streams record end` un buffer después
  - `disconnect` al terminar
- **Output detector:** observa el directorio de grabación; cuando aparece el archivo, sube a object storage y notifica al orquestador.

**Por qué Windows:** HLAE solo corre en Windows, y CS2 + HLAE requieren GPU dedicada para frame pacing decente a 60+ fps.

Ver [`research/02-hlae-integration.md`](../research/02-hlae-integration.md) y [`research/03-recording-options.md`](../research/03-recording-options.md).

## 5. Effects Composer

**Responsabilidad:** dado un raw clip + metadata de la kill (arma, headshot, tick), aplicar efectos visuales según reglas.

**Input por clip:**
- Vídeo raw (mp4 / mkv del HLAE)
- Metadata estructurada (kill info)

**Reglas (en Lua):** ejemplo conceptual:
```lua
on_kill(function(k)
  if k.weapon == "awp" then
    zoom({start = k.tick + 5, duration = 1.0, scale = 1.4})
  elseif is_pistol(k.weapon) then
    flash({tick = k.tick + 2, duration = 0.15, color = "white"})
  end
  if k.headshot then
    slow_motion({around = k.tick, factor = 0.4, duration = 0.6})
  end
end)
```

**Output:** clip con efectos aplicados (sin música todavía).

**Implementación local actual:** `zv-editor` en Go evalúa reglas Lua con `gopher-lua` y construye filtros FFmpeg. Python queda reservado para música/beat sync o filtergraphs más complejos si hacen falta. Ver [`research/04-post-processing.md`](../research/04-post-processing.md).

## 6. Music Mixer

**Responsabilidad:** seleccionar una pista (de un catálogo del usuario o sugerida) y alinear los cortes/efectos al beat.

**Pasos:**
1. Analizar la pista con `librosa.beat.beat_track` → tempo + timestamps de beats.
2. Ajustar los puntos de corte de los segmentos a los beats más cercanos (snap-to-beat).
3. Mezclar audio: la música como base + sidechain ducking sobre los sonidos clave (kill sound, flashbang) para que se "sientan".

Ver [`research/05-music-sync.md`](../research/05-music-sync.md).

## 7. Encoder + Delivery

**Responsabilidad:** rendering final.

- Encode H.264 / H.265 a la resolución/aspect que pida el preset (1920×1080 horizontal, 1080×1920 vertical para reels/shorts).
- Subir a object storage.
- Devolver URL pre-firmada al frontend.

## Tabla de límites entre componentes

| Componente            | Entrada                       | Salida                         | Estado interno      |
|-----------------------|-------------------------------|--------------------------------|---------------------|
| Frontend              | usuario (HTTP)                | requests al orquestador        | UI state            |
| Orquestador           | requests, eventos de workers  | jobs, asignaciones             | DB + cola           |
| Demo Parser           | `.dem` + reglas               | kill plan (JSON)               | ninguno (stateless) |
| Recording Driver      | kill plan + `.dem`            | raw clips                      | proceso CS2 / HLAE  |
| Effects Composer      | raw clip + kill metadata      | clip con efectos               | ninguno             |
| Music Mixer           | clips + pista                 | clip con audio mezclado        | ninguno             |
| Encoder + Delivery    | clip compuesto                | URL final                      | ninguno             |

La idea: si un componente falla, el job se reintenta solo en ese paso, sin volver a parsear o regrabar.
