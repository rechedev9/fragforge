# zackvideo — Documentación

Pipeline para generar highlight reels automáticos de CS2 a partir de archivos `.dem`.

## Estructura

- [`architecture/`](./architecture) — diseño end-to-end del sistema.
  - [`00-overview.md`](./architecture/00-overview.md) — visión general y objetivos.
  - [`01-components.md`](./architecture/01-components.md) — descomposición en módulos.
  - [`02-data-flow.md`](./architecture/02-data-flow.md) — flujo de datos demo → clip final.
  - [`03-stack-decisions.md`](./architecture/03-stack-decisions.md) — decisiones de stack y trade-offs.
  - [`04-deployment.md`](./architecture/04-deployment.md) — topología en VPS + worker Windows por Tailscale.
- [`research/`](./research) — notas de investigación sobre librerías y herramientas externas.
  - [`01-demo-parsing.md`](./research/01-demo-parsing.md) — demoinfocs-golang (parser elegido).
  - [`02-hlae-integration.md`](./research/02-hlae-integration.md) — comandos y scripting de HLAE en CS2 / Source 2.
  - [`03-recording-options.md`](./research/03-recording-options.md) — HLAE `mirv_streams` vs OBS WebSocket.
  - [`04-post-processing.md`](./research/04-post-processing.md) — FFmpeg: concat, zoompan, overlay, mezcla de audio.
  - [`05-music-sync.md`](./research/05-music-sync.md) — librosa para detectar beats y alinear cortes.
  - [`06-prior-art.md`](./research/06-prior-art.md) — proyectos similares y aprendizajes.
- [`specs/`](./specs) — specs de cambios concretos cuando se empiece a implementar.

## Estado

Fase: **foundation local + orquestador inicial implementados**.

Decisiones ya tomadas:
- **Parser de demos:** [`demoinfocs-golang`](https://github.com/markus-wa/demoinfocs-golang).
- **Lenguaje del orquestador y parser:** Go.
- **Topología de despliegue:** VPS Linux del usuario + PC Windows como worker de grabación, unidos por Tailscale.
- **Mecanismo de grabación:** HLAE `mirv_streams` (sin OBS).
- **Frontend:** Next.js + Tailwind.
- **"IA" de efectos:** reglas en Lua (no modelo ML en V1).
- **Cola:** Redis + Asynq.

Specs activos:
- [`specs/2026-05-14-demo-parser-slice.md`](./specs/2026-05-14-demo-parser-slice.md) — `zv-parser` (implementado).
- [`specs/2026-05-14-orchestrator-slice-plan.md`](./specs/2026-05-14-orchestrator-slice-plan.md) — `zv-orchestrator` (implementado).
- [`specs/2026-05-14-hlae-prototype.md`](./specs/2026-05-14-hlae-prototype.md) — HLAE prototype sub-slice (validado en Windows).
- [`specs/2026-05-14-hlae-prototype-plan.md`](./specs/2026-05-14-hlae-prototype-plan.md) — plan ejecutable del prototipo HLAE.
- [`specs/2026-05-15-recorder-low-level-plan.md`](./specs/2026-05-15-recorder-low-level-plan.md) — contrato local de `zv-recorder` + generador JS HLAE (implementado).
- [`specs/2026-05-15-composer-minimal-plan.md`](./specs/2026-05-15-composer-minimal-plan.md) — `zv-composer` local: concatena segmentos grabados en `final.mp4` (implementado).
- [`specs/2026-05-15-local-pipeline-plan.md`](./specs/2026-05-15-local-pipeline-plan.md) — `zv-pipeline` local y primeros workers media del orquestador.
