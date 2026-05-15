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
| `mirv_streams`         | Inicia / detiene grabación; en CS2 usamos `record screen enabled/settings`. **Core de la grabación.** |
| `mirv_campath`         | Cámara cinemática programable (puntos clave + interpolación).   |
| `mirv_input`           | Override del input del juego para mover cámara manual / programada. |
| `mirv_cmd`             | Comando histórico para programar comandos; no es el carrier usado en HLAE 2.x. |
| `mirv_script_load`     | Cargar un fichero JavaScript (Boa JS) al intérprete interno de HLAE 2.x. |
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
            -cmdLine "+playdemo replay.dem +mirv_script_load script.js"
   ```
2. El script `.js` (Boa JavaScript en HLAE 2.x) define la coreografía completa del segmento.
3. Cuando el script termina, ejecuta `disconnect; quit` para cerrar CS2.

## Esqueleto de script por segmento

```js
"use strict";
{
    const id = "zackvideo/generated-recorder";
    const schedule = [
        { tick: 100, key: "seek", cmd: "demo_gototick 102172" },
        { tick: 102236, key: "camera", cmd: "spec_mode 1; spec_player_by_accountid 12345678" },
        { tick: 102294, key: "hide-demoui", cmd: "demoui" },
        { tick: 102300, key: "record-start", cmd: "mirv_streams record start" },
        { tick: 103350, key: "record-end", cmd: "mirv_streams record end" },
        { tick: 103500, key: "disconnect", cmd: "disconnect" },
        { tick: 103600, key: "quit", cmd: "quit" }
    ];
    const fired = {};
    mirv.events.clientFrameStageNotify.on(id, (e) => {
        if (e.isBefore) return;
        const tick = mirv.getDemoTick();
        for (const item of schedule) {
            if (!fired[item.key] && tick >= item.tick) {
                fired[item.key] = true;
                mirv.exec(item.cmd);
            }
        }
    });
    globalThis[id] = {
        unregister: () => mirv.events.clientFrameStageNotify.off(id)
    };
}
```

La condición es `tick >= item.tick`, no igualdad estricta, porque el callback se ejecuta por frame y puede saltar el tick exacto.

## Output de `mirv_streams`

`mirv_streams` en CS2 queda validado con la ruta de screen recording:

```text
mirv_streams record fps 60
mirv_streams record screen enabled 1
mirv_streams record screen settings afxFfmpegYuv420p
```

Para zackvideo V1: HLAE produce `takeNNNN/video.mp4` H.264 y `takeNNNN/audio.wav` por separado. La composición/mux final se hace después con FFmpeg.

## Puntos abiertos a validar en prototipo

1. **Seek/camera lead**: buscar al menos 2 segundos antes de `record start`; fijar POV 1 segundo antes. Sin ese lead, camera y `record start` compiten en el mismo frame.
2. **`host_timescale` + `mirv_streams`**: validado como limpio a 2x en clip corto, pero sin mejora wall-clock material; default V1 queda en 1x.
3. **Múltiples segmentos en una sola sesión de CS2**: validado. Una sesión puede producir varios `takeNNNN`.
4. **POV vs free-cam**: usar `spec_mode 1; spec_player_by_accountid <id>` después del seek lead. Para clips cinemáticos futuros usamos `mirv_campath`.

## Lenguaje de scripts: JavaScript para HLAE

HLAE 2.x expone Boa JavaScript para `mirv_script_load`. El Recording Driver debe generar JS desde datos tipados, no mantener scripts escritos a mano. Las reglas de efectos pueden seguir siendo Lua en una capa posterior; el carrier HLAE es JS.
