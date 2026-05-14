# Arquitectura — Visión general

## Objetivo

Convertir un archivo `.dem` de CS2 (HLTV o Faceit) en un clip vertical/horizontal pulido tipo "AWPGOD" listo para redes, **sin edición manual**: detectar las kills relevantes de un jugador objetivo, grabarlas con cámara de HLAE, añadir efectos (zoom, flash) y sincronizar música viral.

## Principios de diseño

1. **Pipeline de etapas independientes** conectadas por artefactos en disco / cola de jobs. Cada etapa se puede testear y reemplazar por separado.
2. **Demo es la fuente de verdad.** Toda decisión de qué grabar (rango de ticks, cámara, jugador) se deriva del parseo, no de heurísticas sobre el vídeo.
3. **Grabación → composición separadas.** HLAE / OBS solo produce material crudo a alta calidad; los efectos (zoom, flash, música) se aplican en una etapa de composición con FFmpeg posterior.
4. **Reglas de efectos en Lua.** El "look" del clip (qué efecto para qué arma, duración, color grading) vive en scripts Lua editables sin tocar el core.
5. **Frontend desacoplado.** El backend expone una API; el frontend puede ser una SPA web hoy y una app desktop mañana.

## Diagrama de alto nivel

```
                    ┌────────────────────────────────────────────────────────────┐
                    │                       Frontend (Web)                       │
                    │   Upload .dem · elegir jugador · presets · ver resultados  │
                    └─────────────────────────────────┬──────────────────────────┘
                                                      │ HTTPS / WebSocket
                                                      ▼
                    ┌────────────────────────────────────────────────────────────┐
                    │                   API Gateway / Orquestador                │
                    │              (recibe jobs, persiste estado, autoriza)      │
                    └─┬────────────┬────────────┬─────────────┬─────────────┬────┘
                      │            │            │             │             │
                      ▼            ▼            ▼             ▼             ▼
                ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌────────────┐ ┌──────────┐
                │  Demo    │ │ Recording│ │  Effects │ │   Music    │ │ Encoder  │
                │  Parser  │ │  Driver  │ │ Composer │ │   Mixer    │ │  + CDN   │
                │ (Go +    │ │ (HLAE +  │ │ (FFmpeg+ │ │ (librosa + │ │ (FFmpeg) │
                │  demo-   │ │  CS2     │ │  Lua     │ │  FFmpeg)   │ │          │
                │  infocs) │ │  worker) │ │  rules)  │ │            │ │          │
                └──────────┘ └──────────┘ └──────────┘ └────────────┘ └──────────┘
                      │            │            │             │             │
                      └────────────┴────────────┴─────────────┴─────────────┘
                                                │
                                                ▼
                                        Object Storage
                                        (demos, raw clips, output)
```

## Restricciones técnicas conocidas

- **HLAE es Windows-only.** El "Recording Driver" debe correr en una máquina Windows con CS2 instalado. El resto del pipeline puede ser multi-plataforma.
- **CS2 + HLAE no se puede paralelizar en una sola máquina** (una sola instancia de CS2 por GPU). Si queremos throughput hay que escalar horizontalmente con varios workers Windows.
- **Latencia esperada por demo:** la grabación se hace a velocidad de reproducción del demo (puede acelerarse con `host_timescale`, pero con riesgo de glitches visuales). Para un partido de 40 min con 20 kills relevantes, esperar 5–15 min de grabación.
- **Tamaño de demo:** un `.dem` típico va de 30 MB a 200 MB. El raw recording sin compresión puede ser varios GB; encodear a la salida es obligatorio.

## Fuera de alcance (V1)

- Edición manual interactiva. El usuario configura presets y recibe el clip; si quiere retocar, exporta el proyecto.
- Soporte de CS:GO legacy (el parser sí lo soporta, pero CS:GO ya no es el target).
- Música con licencia comercial: V1 usa pistas libres / del usuario; integraciones con Spotify/Epidemic se ven más adelante.
- Auto-subtitles, overlays con stats: posible V2.
