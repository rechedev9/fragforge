# Spec — HLAE Prototype (sub-slice 0 del Recording slice)

**Fecha:** 2026-05-14
**Estado:** propuesto, esperando aprobación del usuario.
**Componente:** Recording Driver (ver [`architecture/01-components.md`](../architecture/01-components.md#4-recording-driver-windows-worker)).
**Predecesores:** `zv-parser` y `zv-orchestrator` ya implementados.

## Objetivo

Validar empíricamente, contra una demo real, las cuatro incógnitas técnicas que bloquean el diseño del binario `zv-recorder`. El sub-slice termina con un documento de findings y un `.mp4` de muestra; **no produce código Go**.

Las incógnitas vienen de [`research/02-hlae-integration.md`](../research/02-hlae-integration.md):

1. Precisión real de `demo_gototick` en CS2 (Source 2).
2. Posibilidad de grabar varios segmentos en una sola sesión de CS2.
3. Formato y calidad del output de `mirv_streams` (codec, fps, tamaño, audio).
4. Compatibilidad de `host_timescale 2` con grabación.

Sin estas respuestas, cualquier código que se escriba para el Recording Driver descansa sobre supuestos no verificados.

## Fuera de alcance

- Cualquier código Go (eso es el Spec 2: `zv-recorder` CLI).
- Integración con `zv-orchestrator` o con Asynq (eso es el Spec 3).
- Tailscale / conectividad VPS ↔ Windows worker.
- Cámaras cinemáticas (`mirv_campath`). Asumimos cámara fija al jugador con `spec_player_by_accountid`.
- Audio mixing y selección de música.
- Demos de FaceIT con voice chat.
- Paralelismo / varios CS2 simultáneos.

## Demo de referencia

Todos los experimentos usan la demo ya commiteada en `testdata/`:

| Campo                | Valor                                                  |
|----------------------|--------------------------------------------------------|
| Archivo              | `testdata/lavked-vs-tnc-m2-nuke.dem`                   |
| Mapa                 | `de_nuke`                                              |
| Tickrate             | 64                                                     |
| Jugador objetivo     | `maaryy` (SteamID64 `76561198148986856`)               |
| AccountID (32 bits)  | `188721128` (para `spec_player_by_accountid`)          |

Kill plan ya validado en `testdata/lavked-vs-tnc-m2-nuke.expected.json`. Los ticks usados por los experimentos salen de ahí.

## Pre-requisitos en el PC Windows

Antes de correr los experimentos hay que verificar:

| Software        | Cómo verificar                                                                    |
|-----------------|-----------------------------------------------------------------------------------|
| CS2             | `Get-Item "C:\Program Files (x86)\Steam\steamapps\common\Counter-Strike Global Offensive\game\bin\win64\cs2.exe"` |
| HLAE            | `HLAE.exe` accesible. Versión >= la que soporte Source 2 (releases >= 2024).      |
| FFmpeg / ffprobe| `ffprobe -version` en PowerShell.                                                 |
| Demo accesible  | `lavked-vs-tnc-m2-nuke.dem` copiada localmente al PC Windows.                     |

Si algo falta, el primer paso del Spec es instalarlo. Las rutas se parametrizan en el runner PowerShell.

## Los cuatro experimentos

### E1 — Precisión de `demo_gototick`

**Pregunta:** cuando pedimos `demo_gototick T`, ¿el primer frame grabado corresponde realmente al tick T? ¿Cuánto se desvía?

**Segmento usado:** `seg-001` (`tick_start = 22086`, primera kill en `tick = 22278`, AK headshot triple en round 4).

**Procedimiento:**

1. Generar un `.mirv` que:
   - Hace seek a tick `22086`.
   - Fija cámara al jugador con `spec_player_by_accountid 188721128`.
   - Inicia grabación 2 segundos antes de la primera kill (en tick `22086`).
   - Detiene grabación 2 segundos después de la primera kill (en tick `22406`).
   - Desconecta y cierra.

2. Correr con `run-experiment.ps1 -Experiment e1`.

3. Inspeccionar el `.mp4`:
   - `ffprobe` para confirmar duración y fps.
   - Visualmente: identificar el frame de la primera kill.
   - Esperado: la kill aparece aproximadamente a `(22278 - 22086) / 64 = 3.0 s` del inicio del clip. Tolerancia ±5 frames a 60 fps (±83 ms).

**Salida a documentar:**

- Duración real del `.mp4` (s).
- Frames totales.
- Tick aparente del primer frame (calculado hacia atrás desde la kill).
- Offset entre tick pedido y tick real.

**Criterio de decisión:**

- Offset ≤ 2 ticks (≈31 ms) → `pre_roll_seconds` actual (3 s) está sobredimensionado, podemos bajar.
- Offset entre 3 y 10 ticks → mantener pre_roll de 3 s.
- Offset > 10 ticks → revisar pre_roll y posiblemente usar `mirv_skip` en lugar de `demo_gototick`.

### E2 — Multi-segmento en una sola sesión de CS2

**Pregunta:** ¿se pueden encadenar `demo_gototick` + `mirv_streams record start/end` varias veces en una misma sesión de CS2 sin reiniciar el proceso? ¿Cada segmento produce un fichero separado?

**Segmentos usados:** `seg-001`, `seg-002`, `seg-003` (todos del round 4-6, gaps de ~36-138 s entre cada uno).

**Procedimiento:**

1. Generar un `.mirv` que:
   - Seek a `22086` → grabar hasta `22868` → cerrar fichero.
   - Seek a `31746` → grabar hasta `32258` → cerrar fichero.
   - Seek a `34586` → grabar hasta `35098` → cerrar fichero.
   - Desconectar y cerrar.

2. Correr con `run-experiment.ps1 -Experiment e2`.

3. Verificar:
   - Existen 3 ficheros de salida (uno por segmento).
   - Cada uno es reproducible (no corrupto).
   - Cada uno cubre el rango temporal esperado.

**Criterio de decisión:**

- 3 ficheros válidos → `zv-recorder` usa "1 sesión CS2 por demo". Throughput óptimo.
- Algún fichero corrupto o solo el primero generado → "1 sesión CS2 por segmento". Más lento pero simple.

### E3 — Formato y calidad del output

**Pregunta:** ¿qué genera `mirv_streams` exactamente cuando lo configuramos con la receta candidata? ¿Es directamente un `.mp4` H.264 o es una secuencia de frames TGA/EXR que hay que componer con FFmpeg?

**Segmento usado:** `seg-002` (1 kill, simple).

**Procedimiento:**

1. Generar un `.mirv` que graba el rango `31746 → 32258` (~8 s).

2. Configurar `mirv_streams` con dos configuraciones candidatas, probadas en orden:
   - **C1 (preferida):** `mirv_streams add ffmpeg main "-c:v libx264 -preset slow -crf 18 -pix_fmt yuv420p -r 60 -s 1920x1080 -y <out>.mp4"`. Si HLAE soporta el pipe a FFmpeg directamente, esto es 1 fichero final.
   - **C2 (fallback):** `mirv_streams add tga main` → genera secuencia de frames TGA. Si C1 falla, queda este camino y FFmpeg se invoca por separado tras la grabación.

   La sintaxis exacta del comando se valida en este experimento; si la documentada arriba no es la real, se actualiza el spec.

3. Inspeccionar el output:
   - ¿Es 1 fichero o muchos?
   - Si es 1 fichero: codec, fps, resolución, bitrate (`ffprobe`).
   - ¿Hay pista de audio? ¿Es del juego (kills, balas) o silencio?
   - Tamaño en disco.

**Criterio de decisión:**

- Output directo H.264 1080p60 con audio → `zv-recorder` consume el `.mp4` tal cual.
- Frames raw + FFmpeg necesario → `zv-recorder` lanza un FFmpeg en pipe junto con HLAE.
- Sin audio → documentar y aceptar (el audio se reemplaza por música, sólo necesitamos placeholder o stems separados).

### E4 — `host_timescale` durante grabación

**Pregunta:** ¿poner `host_timescale 2` antes de `mirv_streams record start` produce vídeo limpio? ¿O introduce frame drops, stuttering, audio glitches?

**Segmento usado:** `seg-001` repetido (comparable con E1).

**Procedimiento:**

1. `.mirv` idéntico al E1 pero con `host_timescale 2` antes de `mirv_streams record start` y `host_timescale 1` después de `mirv_streams record end`.

2. Comparar el `.mp4` resultante con el de E1:
   - Duración del vídeo (debería seguir siendo ~5 s, igual que E1, porque `host_timescale` afecta al wall-clock pero el motor sigue procesando el mismo número de ticks).
   - Número de frames (debería ser idéntico a E1).
   - Wall-clock: cuánto tardó la sesión PowerShell de extremo a extremo (objetivo: ~50% del E1).
   - Inspección visual: ¿hay tearing, frames repetidos, audio acelerado o desincronizado?

**Criterio de decisión:**

- Limpio y wall-clock ~50% del E1 → usar `host_timescale 2` por defecto en `zv-recorder`. Throughput x2.
- Glitches visuales o frame drops → quedarse en `host_timescale 1`.
- Resultado intermedio (limpio pero misma duración wall-clock) → no merece la pena, dejar en 1×.

## Artefactos a entregar (yo)

```
zackvideo/
├── scripts/
│   └── hlae/
│       ├── README.md                  ← guía paso a paso para el usuario
│       ├── run-experiment.ps1         ← runner PowerShell
│       ├── e1-seek-accuracy.mirv
│       ├── e2-multi-segment.mirv
│       ├── e3-output-format.mirv
│       └── e4-host-timescale.mirv
└── docs/
    ├── specs/
    │   └── 2026-05-14-hlae-prototype.md          ← este documento
    └── research/
        └── 07-hlae-prototype-results.md.template ← plantilla para findings
```

### Esqueleto del runner PowerShell

`run-experiment.ps1` acepta:

```powershell
.\run-experiment.ps1 `
  -Experiment <e1|e2|e3|e4> `
  -Demo "<path al .dem>" `
  [-Cs2Exe "<path al cs2.exe>"] `
  [-HlaeExe "<path a HLAE.exe>"] `
  [-OutDir "<dir>"]
```

Defaults razonables para `Cs2Exe` y `HlaeExe`. Crea `OutDir` (por defecto `$env:TEMP\zv-hlae\<experiment>\`), lanza HLAE con el `.mirv` correspondiente, espera a que el proceso termine, lista los ficheros generados.

### Estructura de cada `.mirv`

Listas planas de `mirv_cmd add tick N "<command>"`. Sintaxis exacta a confirmar en el primer experimento; el spec asume la documentada en [`research/02-hlae-integration.md`](../research/02-hlae-integration.md). Si en el primer intento la sintaxis no funciona, se actualiza el spec.

Convenciones compartidas por los cuatro experimentos:

- `spec_player_by_accountid 188721128` se ejecuta antes de cada `mirv_streams record start` para asegurar que la cámara está fijada al jugador objetivo.
- El comando final de cada `.mirv` es siempre `quit`, programado unos ticks después del último `record end`.
- Los ticks programados antes de `tick_start` del segmento (e.g. `tick 50` para `demo_gototick`) son arbitrarios — solo necesitan ser tempranos.

Ejemplo conceptual para `e1-seek-accuracy.mirv`:

```
mirv_cmd add tick 50    "demo_gototick 22086"
mirv_cmd add tick 100   "spec_player_by_accountid 188721128"
mirv_cmd add tick 22086 "mirv_streams record start"
mirv_cmd add tick 22406 "mirv_streams record end"
mirv_cmd add tick 22500 "disconnect"
mirv_cmd add tick 22600 "quit"
```

### Plantilla de findings

`07-hlae-prototype-results.md.template` tiene una sección por experimento con tablas vacías para rellenar:

- Comando exacto ejecutado.
- Output de `ffprobe`.
- Notas visuales (¿se ve la kill? ¿en qué frame?).
- Captura del primer y último frame (paths a screenshots no commiteados).
- Veredicto (✅/❌) y decisión derivada para el Spec 2.

## Artefactos a entregar (usuario, en su PC)

1. Correr los 4 experimentos en orden.
2. Llenar `07-hlae-prototype-results.md` (renombrado desde el `.template`) con los resultados.
3. Guardar 1 `.mp4` de muestra (el de E2, idealmente, porque cubre el caso multi-segmento) en algún storage accesible (Drive, S3, disco compartido) y poner el enlace en el doc.
4. Commitear el doc completado.

## Criterios de cierre del sub-slice

El prototipo se da por cerrado cuando se cumplen las cuatro:

- [ ] `07-hlae-prototype-results.md` commiteado con veredicto explícito para cada experimento.
- [ ] Para los ✅: parámetros confirmados anotados (pre_roll real, codec, fps, decisión sobre host_timescale).
- [ ] Para los ❌: workaround propuesto en una sección "Restricciones para el Spec 2".
- [ ] Link a un `.mp4` de muestra accesible.

Con eso, el Spec 2 (`zv-recorder` CLI) se puede diseñar con datos reales en lugar de suposiciones.

## Errores y casos borde

| Caso                                            | Mitigación                                            |
|-------------------------------------------------|-------------------------------------------------------|
| HLAE no se inyecta (versión incompatible)       | Documentar versión exacta y rebajar si hace falta.    |
| Sintaxis `mirv_cmd add tick` no es la real      | Actualizar este spec y los `.mirv` antes de seguir.   |
| CS2 crashea durante el seek                     | Logear stack trace, reducir velocidad de seek.        |
| `mirv_streams` no produce nada                  | Probar config alternativa con frames raw + FFmpeg.    |
| El PC del usuario no rinde a 60 fps             | Bajar a 30 fps el primer experimento, anotar.         |
| Demo en CS:GO legacy (no este caso)             | N/A — la demo elegida es CS2.                          |

## Qué viene después

Cuando este sub-slice cierre:

- **Spec 2 — `zv-recorder` CLI:** binario Go (`GOOS=windows`) que toma un kill plan + `.dem` + `output_dir` y produce los `.mp4`. Pure local CLI. Basado en los hallazgos del prototipo.
- **Spec 3 — Integración orchestrator + worker pull:** nueva task Asynq `record:demo`, modo "worker" del binario que hace long-poll al orquestador por Tailscale, upload de clips, transiciones de estado del job.

## Aprobación pendiente

- [ ] Confirmar que `seg-001`/`seg-002`/`seg-003` de `lavked-vs-tnc-m2-nuke.dem` son segmentos adecuados (ningún warmup, tickrate correcto, segmentos lo bastante separados).
- [ ] Confirmar que el usuario tiene HLAE instalado o está dispuesto a instalarlo.
- [ ] Confirmar que dedicar una tarde a correr 4 experimentos y rellenar el doc es el formato deseado (vs. una iteración más automatizada).
