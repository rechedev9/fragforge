# Arquitectura — Flujo de datos

Recorrido completo de una request, paso a paso, con los artefactos que viajan entre componentes.

## 1. Upload

```
Usuario --(multipart POST /jobs)--> Frontend --(stream)--> Orquestador --(PUT)--> Object Storage
       \-------- config JSON -----------/
```

- El frontend hace upload en streaming directo al object storage (URL pre-firmada) para evitar pasar el `.dem` por el servidor.
- El orquestador recibe `{ demo_key, target_steamid64, preset_id, music_choice }` y crea un job.

## 2. Parseo

```
Orquestador --(job: parse)--> Demo Parser Service
Demo Parser  --(GET .dem)----> Object Storage
Demo Parser  --(kill plan)---> Orquestador
```

- El parser corre en cualquier worker Linux (Go binary).
- El kill plan se guarda en la DB y se vuelve consultable desde el frontend ("vimos 12 kills, 4 con AWP, 3 con USP, ...").
- **Gate de usuario** opcional aquí: el frontend puede mostrar el plan y dejar al usuario aprobar/editar antes de grabar. Útil porque grabar es la fase más cara.

## 3. Grabación

```
Orquestador --(job: record, kill plan)--> Recording Driver (Windows worker)
RecordingDrv --(GET .dem)----------------> Object Storage
RecordingDrv --(lanza CS2 + HLAE + Lua script)
HLAE         --(mirv_streams record)-----> archivo .mp4 / .mkv local
RecordingDrv --(PUT raw clip por segmento)-> Object Storage
RecordingDrv --(progress events)----------> Orquestador
```

- El driver genera, por cada segmento del kill plan, un script Lua con timestamps absolutos (ticks de inicio/fin) y lo carga con `mirv_script_load`.
- Una sesión de CS2 procesa toda la demo de corrido: ahorra el coste de cargar el mapa/demo varias veces.
- Si el clip cubre múltiples segmentos cercanos, se graban en un solo archivo con marcadores; si están separados, se hacen sub-clips.

## 4. Composición de efectos

```
Orquestador --(job: compose, per-segment)--> Effects Composer
Composer    --(GET raw clip + metadata)----> Object Storage
Composer    --(lua rules)-------------------> aplica zoom, flash, slow-mo, color grade
Composer    --(PUT composed clip)----------> Object Storage
```

- Los efectos se aplican **por segmento**, no sobre el clip final concatenado. Razón: el FFmpeg filtergraph es más simple y reusa fácilmente caché de segmentos individuales si el usuario re-renderiza con otra música.

## 5. Mezcla de música + corte al beat

```
Orquestador --(job: mix)----------> Music Mixer
Mixer       --(GET clips + pista)--> Object Storage
Mixer       --(librosa beat_track)
Mixer       --(snap segmentos a beats)
Mixer       --(FFmpeg concat + amix)
Mixer       --(PUT final clip)-----> Object Storage
```

- En este paso es donde decidimos el orden y los puntos de corte definitivos. Antes los segmentos eran independientes; aquí se alinean al beat de la música elegida.

## 6. Encode final + delivery

```
Orquestador --(job: encode)--> Encoder
Encoder     --(FFmpeg H.264 / H.265, target resolution & aspect)
Encoder     --(PUT output)---> Object Storage
Encoder     --(URL pre-signed)-> Orquestador --> Frontend
```

## Modelo de datos mínimo

```
Job
  id (uuid)
  user_id
  demo_key
  target_steamid64
  preset_id
  music_key
  status            -- queued | parsing | parsed | recording | composing | mixing | encoding | done | failed
  failure_reason
  created_at / updated_at

KillPlan
  job_id
  tickrate, map
  segments (jsonb)

Artifact
  job_id
  kind              -- raw_clip | composed_clip | mixed_clip | final
  segment_id (nullable)
  storage_key
  size, duration
  created_at
```

## Estados y reintentos

- Cada job avanza por estados de forma idempotente. Cada worker chequea: ¿este artefacto ya existe? → skip.
- Reintentos automáticos en parsing/composing/mixing/encoding (operaciones puras). La grabación NO se reintenta automáticamente porque cuesta minutos y GPU; se marca `failed` y el usuario decide.
- El kill plan es el contrato: una vez aprobado, los pasos siguientes no vuelven a tocar la demo.
