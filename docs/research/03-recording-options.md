# Research — Opciones de grabación: HLAE `mirv_streams` vs OBS WebSocket

## Resumen

| Criterio                           | HLAE `mirv_streams`                          | OBS + obs-websocket                          |
|------------------------------------|-----------------------------------------------|----------------------------------------------|
| Calidad de frame                   | Determinista, sin drops (renderiza on-demand) | Captura en tiempo real, susceptible a drops  |
| Latencia / overhead                | Bajo (mismo proceso del juego)                | Captura externa (DXGI / NVENC)               |
| Control programático               | Vía `mirv_cmd` / script Lua + CLI args        | API WebSocket bien definida desde cualquier lenguaje |
| Soporte multi-escena / overlays    | No                                            | Sí                                            |
| Ecosistema / ejemplos              | Limitado pero específico de CS                | Enorme (streaming, gaming)                    |
| Plataforma                         | Solo Windows (HLAE)                           | Solo Windows en la práctica (OBS multiplataforma pero CS2/HLAE no) |

## Pros / contras detallados

### HLAE `mirv_streams`

**Pros**
- Renderiza frame por frame de forma síncrona con el motor → calidad consistente independientemente de la carga del sistema.
- Se puede grabar a buffers separados: color, depth, normales, máscaras (útil para efectos post con compositing avanzado en V2).
- Hace el pipe directo a FFmpeg → menos pasos.

**Contras**
- API es "config + comandos en consola", no hay un protocolo formal.
- Debuggear es por logs de HLAE; menos visibilidad que OBS.

### OBS + obs-websocket

**Pros**
- API formalizada vía WebSocket (`obsproject/obs-websocket`). Hay clientes Go, Python, JS, C#, .NET.
- Soporte para escenas, overlays (HUD del HLTV, marca del cliente, etc.) sin tocar FFmpeg.
- Hay precedente: el proyecto Keanoski/CS2-Highlight-Automator-for-OBS usa OBS WebSocket + DemoFile.Game.Cs.

**Contras**
- Captura en tiempo real → si el SO scheduling fluctúa, drop de frames.
- El stream no puede ir más rápido que tiempo real (sin lo que HLAE permite con `host_timescale`).

## Recomendación para zackvideo

**Hybrid**: HLAE controla cámara + grabación con `mirv_streams`, OBS no participa en V1.

Razones:
1. Queremos un producto "profesional" → consistencia visual importa más que la flexibilidad de escenas.
2. Los overlays (logos, marcas de agua, stats) los aplicamos en post con FFmpeg, donde son deterministas y más fáciles de iterar.
3. Reducimos componentes en el Windows worker (no necesitamos OBS instalado ni configurado).
4. Si más adelante queremos hacer streams "en vivo" de generación de clips, ahí sí OBS WebSocket puede entrar.

## Detalles de implementación del Recording Driver

```
                Windows worker
┌─────────────────────────────────────────────────────────────┐
│  Recording Driver (Go)                                      │
│    1. Descarga .dem desde object storage                    │
│    2. Genera script Lua por segmento (templating)           │
│    3. Lanza HLAE.exe con -cmdLine "...mirv_script_load..."  │
│    4. Espera a que HLAE termine (mirv_cmd quit)             │
│    5. Sube .mp4 resultante por segmento                     │
│    6. Limpia archivos temporales                            │
└─────────────────────────────────────────────────────────────┘
```

**Modo "una sesión, varios segmentos":** un solo script Lua agrega todos los segmentos del kill plan con seeks intermedios entre cada uno. CS2 carga el mapa una vez, lo que ahorra 30–60s por segmento. **A validar en prototipo:** que `mirv_streams record end` cierre el archivo, y que un `start` posterior no corrompa nada.

## Throughput estimado

- Cargar mapa + buffer inicial: ~45s.
- Grabar 10s de gameplay a 60fps 1080p: ~10s reales (sin `host_timescale`).
- Cerrar archivo y siguiente seek: ~5s.
- → Para 10 segmentos de 10s cada uno en una demo: ~45 + 10×15 = ~3 min de wall-clock.

Si `host_timescale 2` funciona limpio: bajamos a ~2 min.

Eso por máquina, secuencial. Paralelismo = más máquinas Windows. Para V1 una sola máquina alcanza.
