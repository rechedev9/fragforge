# Arquitectura — Decisiones de stack y trade-offs

## Decididas

### Parser de demos: `demoinfocs-golang`
**Estado:** ✅ Confirmado por el usuario.

- Repo: https://github.com/markus-wa/demoinfocs-golang
- Soporta CS2 y CS:GO, basado en eventos (`events.Kill`, `events.RoundStart`, etc.).
- Acceso a `GameState()` para posiciones live, scores, granadas en vuelo.
- Implica que el **Demo Parser Service es un binario Go**.

### Lenguaje del orquestador: **Go**
**Estado:** ✅ Confirmado por el usuario el 2026-05-14.

| Opción          | Pros                                                                  | Contras                                                |
|-----------------|-----------------------------------------------------------------------|--------------------------------------------------------|
| **Go** (elegido)| Mismo lenguaje que el parser → comparten tipos. Ecosistema HTTP fuerte (chi, gin, fiber). Binario estático ideal para deploy en VPS. | (ninguno bloqueante)                                    |
| C++ (Drogon / Crow) | Performance top.                                                  | Mucho boilerplate, build/deploy más caros, memoria insegura, iteración lenta. |
| Python (FastAPI)    | Iteración rapidísima.                                             | Otro lenguaje más en el stack.                          |

**Por qué Go:**
- Mismo lenguaje que el Demo Parser Service → monorepo Go con tipos compartidos, sin capa de serialización entre los dos.
- Deploy en el VPS Linux es trivial: binario estático + `systemd` + `nginx`. Sin libc/glibc surprises.
- El cuello de botella del pipeline es FFmpeg / CS2 / disco, no la capa HTTP. Go alcanza y sobra.
- Velocidad de iteración alta (compila en segundos, `go test` rápido).

**Por qué NO C++:** no aporta beneficio medible para este workload (HTTP + cola + DB), suma fricción de build, deploy y memoria insegura, y baja la velocidad de iteración. Si en el futuro aparece un componente que de verdad lo necesite (no aparece hoy), se evalúa entonces.

**Por qué NO Python en el orquestador:** queremos compartir tipos con el parser y mantener el orquestador independiente del runtime de Python (que sí usa el composer/mixer/encoder). Mezclar todo en Python sería más simple pero sacrifica la separación clara entre "core" (Go) y "media" (Python).

**Resumen del rol de cada lenguaje:**
- **Go (core):** orquestador, parser, recording driver controller.
- **Go (media local):** editor local 9:16 + Lua effects embebido con `gopher-lua`.
- **Python (media futuro):** mixer/beat sync/encoder avanzado si librosa o filtergraphs complejos lo justifican.
- **Lua (scripting):** reglas de efectos de post-procesado.
- **JavaScript (HLAE):** carrier generado para `mirv_script_load` en HLAE 2.x.
- **TypeScript (UI):** Next.js frontend.
- **C++:** fuera del V1.

## Pendientes de decidir

### 1. Mecanismo de grabación

| Opción                        | Pros                                                                                      | Contras                                                          |
|-------------------------------|-------------------------------------------------------------------------------------------|------------------------------------------------------------------|
| **HLAE `mirv_streams`** (directo a FFmpeg) | Sin captura externa. Calidad determinista (frames "puros", no afectado por carga del sistema). Soporta múltiples buffers (color, depth, etc.). | Más opaco que OBS. Sin overlay / scene mixing.                    |
| **OBS WebSocket** + HLAE para cámara | Más flexible (escenas, overlays, streaming). Mucho material disponible (Keanoski/CS2-Highlight-Automator-for-OBS).               | Captura "real-time" → dropped frames si el sistema se carga.       |
| **Hybrid:** HLAE para cámara + `mirv_streams` para output | Lo mejor: cámara fija con HLAE, captura sin pérdida con `mirv_streams`, post-edit con FFmpeg. | Configuración inicial más compleja.                            |

**Recomendación:** **Hybrid (HLAE + mirv_streams).** Razón: la calidad determinista importa para un producto "profesional"; OBS introduce variabilidad. Los overlays/escenas se aplican en post-edit con FFmpeg, donde tenemos control total.

### 3. Frontend

| Opción         | Pros                                                            | Contras                                |
|----------------|-----------------------------------------------------------------|----------------------------------------|
| **Next.js + Tailwind** | Estándar, mucho ejemplo, deploy fácil (Vercel / Railway).  | Otro lenguaje (TS).                    |
| **SvelteKit**  | Bundle más chico, DX agradable.                                 | Menos ejemplos para video pipelines.   |
| **App desktop (Tauri)** | Si el usuario lo va a usar local, evita servidores.     | Distribución multi-plataforma molesta. |

**Recomendación:** **Next.js + Tailwind** para V1 (web app). Si más adelante el usuario lo quiere local, envolvemos con Tauri sin reescribir.

### 4. Estrategia de "IA" para post-edit

El usuario habla de "programar una IA" para los efectos. Hay dos interpretaciones:

**Opción A — Reglas + Lua (recomendada V1):** los efectos son determinísticos. AWP → zoom, pistola → flash, headshot → slow-mo. Reglas en Lua, fáciles de iterar visualmente. No es "IA" estrictamente, pero hace lo que el usuario describe.

**Opción B — Modelo ML (V2+):** entrenar un modelo (o usar uno existente) que mire el clip y decida los puntos de énfasis, similar a Eklipse. Mucho más caro de construir y probablemente innecesario si la metadata de kills (arma, headshot, ronda, win/loss) ya nos da el 95% de las decisiones.

**Recomendación:** **A para V1**, dejar B como exploración futura. Las "reglas" cubren todo lo que pidió el usuario en el brief inicial.

### 5. Job queue / cola

| Opción       | Pros                                                  | Contras                            |
|--------------|-------------------------------------------------------|-------------------------------------|
| **Asynq** (Go, sobre Redis)   | Simple, dashboard incluido, ya tenemos Redis. | Solo Go.                           |
| **NATS JetStream**            | Mensajería seria, multi-lenguaje, durable. | Más operativa.                     |
| **Postgres + LISTEN/NOTIFY**  | Sin infra extra.                          | No escala más allá de 1 DB.        |

**Recomendación:** **Asynq + Redis** para V1 (el orquestador es Go, los workers no-Go consumen vía HTTP del orquestador). Si necesitamos pub/sub multi-consumer, migramos a NATS.

## Resumen del stack propuesto (V1)

```
Frontend ............ Next.js 15 + Tailwind 4
Orquestador ......... Go (chi + sqlx + asynq)              ← decidido
Demo Parser ......... Go (demoinfocs-golang)               ← decidido
Recording Driver .... Go controller + JavaScript generado para HLAE (Windows)
Effects Composer .... Go local editor + gopher-lua + FFmpeg
Music Mixer ......... Python futuro (librosa + FFmpeg) o Go si no hace falta librosa
Encoder ............. FFmpeg desde el editor/composer local
DB .................. PostgreSQL
Object Storage ...... MinIO en el VPS (S3-compatible)
Cola ................ Redis + Asynq
Deploy .............. VPS Linux del usuario (services Linux)
                      + PC Windows del usuario por Tailscale (Recording Driver)
```

**C++ queda fuera del V1 explícitamente.** Razones: ningún componente del pipeline lo requiere y empujarlo añade fricción sin beneficio medible. Si el usuario quiere preservar la preferencia, el orquestador es el candidato más razonable para reescribir en C++ más adelante, pero no V1.

**Lua sí está presente** en las reglas de efectos visuales del composer/editor. HLAE 2.x quedó validado con Boa JavaScript para `mirv_script_load`; por tanto, el carrier de grabación es JS generado desde Go, no Lua.
