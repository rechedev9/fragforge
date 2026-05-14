# Research — Integración con HLAE (Source 2 / CS2)

Referencias:
- Docs oficiales: https://doc.hlae.site/commands/AfxHookSource2/introduction
- Wiki: https://github.com/advancedfx/advancedfx/wiki/Source2:Commands
- Releases: https://github.com/advancedfx/advancedfx/releases

## Qué es HLAE

Half-Life Advanced Effects es un inyector / hook para Source y Source 2 (CS:GO / CS2) que agrega control fino de cámara, captura raw de vídeo y un intérprete de scripts para automatización. Solo Windows.

## Comandos relevantes (Source 2 — CS2)

| Comando                | Para qué nos sirve                                              |
|------------------------|-----------------------------------------------------------------|
| `mirv_streams`         | Inicia / detiene grabación; salida directa a FFmpeg, formatos crudos. **Core de la grabación.** |
| `mirv_campath`         | Cámara cinemática programable (puntos clave + interpolación).   |
| `mirv_input`           | Override del input del juego para mover cámara manual / programada. |
| `mirv_cmd`             | Programar comandos a ejecutar en un tick / tiempo específico.   |
| `mirv_script_load`     | Cargar un fichero de script (Lua-like) al intérprete interno.   |
| `mirv_script_exec`     | Ejecutar código directamente en el intérprete.                  |
| `mirv_skip`            | Saltar en el demo con más precisión que `demo_gototick`.        |
| `mirv_listentothis`    | Frontend de `tv_listen_voice_indices*` para audio del jugador específico (útil para demos FaceIT). |

Comando Source nativo aún útil:
- `demo_gototick <tick>` — ir a un tick concreto.
- `host_timescale <n>` — acelerar la reproducción para reducir tiempo de grabación (cuidado con frame pacing).
- `spec_player_by_accountid <accountID>` — fijar la cámara al jugador objetivo.

## Patrón de automatización

HLAE no expone una API externa (no hay WebSocket / RPC). El mecanismo oficial:

1. Lanzar CS2 con HLAE inyectado vía CLI:
   ```powershell
   HLAE.exe -csgoLauncher -noGui -autoStart `
            -hookDllPath ".\AfxHookSource2.dll" `
            -programPath "C:\Path\To\cs2.exe" `
            -cmdLine "+playdemo replay.dem +mirv_script_load script.mirv"
   ```
2. El script `.mirv` (sintaxis tipo Lua, intérprete propio) define la coreografía completa del segmento.
3. Cuando el script termina, ejecuta `disconnect; quit` para cerrar CS2.

## Esqueleto de script por segmento

```
// programar el seek y el inicio de grabación en el primer frame
mirv_cmd add tick 100 "demo_gototick 102340"
mirv_cmd add tick 102200 "spec_player_by_accountid 12345678"
mirv_cmd add tick 102300 "mirv_streams record start"
mirv_cmd add tick 103350 "mirv_streams record end"
mirv_cmd add tick 103500 "disconnect"
mirv_cmd add tick 103600 "quit"
```

(La sintaxis exacta de `mirv_cmd` se valida contra https://doc.hlae.site/commands/AfxHookSource/mirv_cmd — la página de CS2 reutiliza el mismo concepto pero los detalles de "tick" vs "time" hay que verificarlos en el primer prototipo.)

## Output de `mirv_streams`

`mirv_streams` puede:
- Escribir frames raw a una pipe que alimenta FFmpeg en otro proceso.
- O escribir directamente archivos (TGA / EXR para vídeo de calidad cinematográfica).

Para zackvideo V1: pipe a FFmpeg → mp4 H.264 (`libx264 -preset slow -crf 18`), 1080p60. Suficiente para luego post-procesar sin pérdida visible.

## Puntos abiertos a validar en prototipo

1. **Exactitud del seek**: `demo_gototick` no siempre es frame-exact en CS2; puede haber ±1 tick. Solución: empezar a grabar 1–2s antes y cortar en post.
2. **`host_timescale` + `mirv_streams`**: hay que ver si la grabación queda limpia a 2× / 4× o si introduce artifacts. Si funciona, reducimos drásticamente el tiempo wall-clock por job.
3. **Múltiples segmentos en una sola sesión de CS2**: validar que se puede `mirv_streams record start/end` varias veces sin reiniciar el proceso. Si sí, ahorra cargar mapa N veces.
4. **POV vs free-cam**: para clips de jugador, fijar cámara al jugador objetivo con `spec_player_by_accountid` es lo simple. Para clips "cinemáticos" usamos `mirv_campath` con puntos calculados a partir de la trayectoria del jugador del kill plan.

## Lenguaje de scripts: Lua

HLAE expone un intérprete tipo Lua. Esto encaja con la preferencia del usuario por Lua para scripting. Los scripts se cargan con `mirv_script_load <path>`. Los podemos generar dinámicamente desde el Recording Driver con templating.
