# Spec — Demo Parser Slice (`zv-parser`)

**Fecha:** 2026-05-14
**Estado:** propuesto, esperando aprobación.
**Componente:** Demo Parser Service (ver [`architecture/01-components.md`](../architecture/01-components.md#3-demo-parser-service-go)).

## Objetivo

Construir el primer binario implementable de zackvideo: una herramienta CLI en Go (`zv-parser`) que toma un archivo `.dem` y emite un **kill plan** JSON con los segmentos relevantes para el jugador objetivo. Sin dependencia de orquestador, DB, ni cola — un binario puro, testeable de forma aislada.

Es el primer slice porque:

- Es la pieza de la que dependen TODAS las demás (sin kill plan no hay nada que grabar).
- No depende de Windows / HLAE / GPU → se puede iterar en cualquier máquina.
- La lib (`demoinfocs-golang`) ya está confirmada y bien documentada.
- Se valida con tests unitarios sin necesidad de demos reales.

## Fuera de alcance de este slice

- API HTTP (la integración con el orquestador llega en el slice siguiente).
- Subida/descarga a object storage (asumimos rutas locales).
- Persistencia en DB.
- Detección de "highlights cinemáticos" (ace, clutch, multi-kill 1vsX) — V1 cubre solo "kills del jugador X agrupadas".
- Selección automática del jugador (en este slice el SteamID se pasa por CLI).
- Soporte de demos GOTV antiguos (CS:GO) — el target son demos CS2 (Source 2) descargados de HLTV o Faceit. La lib soporta ambos pero validamos solo CS2.

## Contrato CLI

```text
zv-parser parse \
  --demo <path>          # ruta a un .dem local
  --steamid <id>         # SteamID64 del jugador objetivo
  --rules <path>         # opcional: JSON con reglas de segmentación
  --out <path>           # ruta de salida del JSON, "-" para stdout
  [--verbose]
```

Códigos de salida:

- `0` éxito.
- `2` argumentos inválidos.
- `3` archivo `.dem` no encontrado o ilegible.
- `4` `.dem` corrupto / no parseable.
- `5` el SteamID indicado no aparece en la demo.
- `1` error inesperado.

Logs en `stderr`, JSON limpio en `stdout` cuando `--out -`.

## Reglas de segmentación

Estructura del JSON de reglas (todos los campos opcionales con defaults):

```json
{
  "weapons": ["awp", "deagle", "ak47", "m4a1", "m4a1_silencer", "usp_silencer", "glock", "hkp2000"],
  "min_kills_in_window": 1,
  "window_seconds": 8,
  "pre_roll_seconds": 3,
  "post_roll_seconds": 5,
  "include_headshot_only": false,
  "exclude_team_kills": true,
  "min_round": 1,
  "max_round": null
}
```

Defaults exactamente como arriba.

**Algoritmo:**

1. Recorrer el `.dem` con `demoinfocs.ParseFile` y handler de `events.Kill`.
2. Filtrar kills donde `Killer.SteamID64 == target` y arma ∈ `weapons` y se cumplan los demás filtros.
3. Convertir cada tick a segundos: `seconds = tick / tickrate`.
4. Agrupar kills consecutivas en "ventanas": dos kills consecutivas pertenecen a la misma ventana si `seconds(k_n) - seconds(k_{n-1}) ≤ window_seconds`.
5. Descartar ventanas con menos de `min_kills_in_window` kills.
6. Para cada ventana, calcular:
   - `tick_start = floor((seconds(first_kill) - pre_roll_seconds) * tickrate)` (clamp a >= 0)
   - `tick_end = ceil((seconds(last_kill) + post_roll_seconds) * tickrate)`
7. Si dentro del rango [tick_start, tick_end] cae un `events.RoundEnd`, recortar `tick_end` al tick del round end (no queremos grabar el resumen post-ronda).
8. Asignar `round` = la ronda en la que ocurrió la primera kill de la ventana.

## Esquema del kill plan (output)

```json
{
  "schema_version": "1.0",
  "generated_at": "2026-05-14T17:42:00Z",
  "demo": {
    "path": "/abs/path/to/demo.dem",
    "sha256": "abc123...",
    "map": "de_inferno",
    "tickrate": 64,
    "duration_ticks": 285000
  },
  "target": {
    "steamid64": "76561198000000000",
    "name_in_demo": "MARTINEZSA",
    "team_at_start": "CT"
  },
  "rules": { ... rules used ... },
  "segments": [
    {
      "id": "seg-001",
      "round": 7,
      "tick_start": 102340,
      "tick_end": 103200,
      "kills": [
        {
          "tick": 102450,
          "weapon": "awp",
          "headshot": true,
          "wallbang": false,
          "victim": {
            "steamid64": "76561198000000001",
            "name_in_demo": "Player2",
            "team_at_kill": "T"
          },
          "killer_pos": [123.4, 456.7, 89.0],
          "victim_pos": [125.1, 470.2, 89.0]
        }
      ]
    }
  ],
  "stats": {
    "total_kills_target": 24,
    "kills_after_filters": 17,
    "segments_created": 8,
    "duration_seconds_total": 92.5
  }
}
```

Notas:

- `schema_version` permite que los workers downstream rechacen versiones que no entienden.
- `sha256` del `.dem` es la fuente de identidad del trabajo: dos jobs con la misma demo + mismo target + mismas rules deben dar el mismo plan (idempotencia).
- `name_in_demo` es informativo (mostrarlo en UI); la identidad estable es `steamid64`.

## Errores y casos borde

| Caso                                       | Comportamiento                                                   |
|--------------------------------------------|------------------------------------------------------------------|
| `.dem` no existe                           | exit 3, mensaje `demo file not found: <path>`                    |
| `.dem` corrupto                            | exit 4, mensaje del parser                                       |
| El SteamID no aparece                      | exit 5, mensaje `target steamid not found in demo`               |
| El SteamID aparece pero no tiene kills     | exit 0, plan con `segments: []` y `stats.kills_after_filters: 0` |
| Reglas con `weapons: []`                   | error de validación, exit 2                                      |
| Demo de CS:GO (no CS2)                     | warning a stderr, parsea igual                                   |
| `tickrate` no detectable                   | exit 4 (raro, sería demo malformada)                             |

## Estructura del repo (este slice)

```text
zackvideo/
├── go.mod
├── cmd/
│   └── zv-parser/
│       └── main.go              ← entrypoint CLI
├── internal/
│   ├── parser/                  ← lógica de parseo + agrupación
│   │   ├── parser.go
│   │   ├── parser_test.go
│   │   ├── segmentation.go
│   │   └── segmentation_test.go
│   ├── killplan/                ← tipos y serialización del plan
│   │   ├── types.go
│   │   └── types_test.go
│   └── rules/                   ← parseo y validación del JSON de reglas
│       ├── rules.go
│       └── rules_test.go
└── testdata/
    └── README.md                ← cómo aportar demos de test (no se commitean)
```

`internal/` para que nadie lo importe externamente todavía. Cuando llegue el slice del orquestador, los tipos públicos pasan a `pkg/`.

## Estrategia de tests

Tres niveles:

**Unit (sin .dem real)** — usando `pkg/demoinfocs/fake`:

- Agrupación: distintas distribuciones de kills (todas juntas, separadas, justo en el límite del window).
- Filtros: armas, headshot only, round range, team kills.
- Bordes: 0 kills, 1 kill, kill en el último tick.
- Validación de rules: campos inválidos, tipos malos.

**Golden tests (con .dem reales)** — `testdata/` ignorado por git, los demos los aporta cada dev local. Compara el plan generado contra `<demo>.expected.json`.

**Smoke test** — un script bash que descarga 1 demo público de HLTV (link conocido) y verifica que `zv-parser parse` produzca un JSON válido contra el schema.

Mínimo para mergear: unit tests + 1 golden con la demo del usuario (`gentle-mates-vs-magic-m2-inferno...`).

## Qué viene después de este slice

El slice siguiente (no parte de este spec):

- Empaquetar `zv-parser` como worker Asynq que consume jobs del orquestador en vez de leer de CLI.
- Subir el plan a Postgres + emitir evento al frontend.

Pero eso ya requiere tener al menos un esqueleto de orquestador. Por eso lo separamos: este spec se puede implementar y mergear sin nada del resto.

## Aprobación pendiente

Antes de empezar a implementar:

- [ ] Confirmar el schema del kill plan (especialmente nombres de campos `weapons`, `victim.team_at_kill`, etc.).
- [ ] Confirmar los defaults de las reglas (¿8s de window? ¿3s pre / 5s post?).
- [ ] Confirmar que la primera demo de prueba será la que ya tiene el usuario (`gentle-mates-vs-magic-m2-inferno_...dem`) — hace falta una copia accesible para testear localmente.
