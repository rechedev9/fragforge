# Research — Parseo de demos con `demoinfocs-golang`

**Decisión adoptada:** este es el parser elegido por el usuario para zackvideo.

Repo: https://github.com/markus-wa/demoinfocs-golang
Versión actual al momento de la investigación: v5.

## Por qué este parser

- Maduro, Go-nativo, basado en eventos (no requiere materializar toda la demo).
- Soporta CS2 (Source 2) y CS:GO en el mismo API.
- 300+ snippets de ejemplo en Context7. Source reputation: High.
- Comparado con alternativas:
  - **awpy** (Python, polars dataframes) — más ergonómico para análisis ad-hoc pero pierde por velocidad y no es event-driven.
  - **LaihoE/demoparser** (Rust + Python/JS) — más rápido sobre bulk de demos, pero menos eventos expuestos y la integración Go es secundaria.

## Patrón de uso clave para zackvideo

El servicio de parsing es stateless: recibe `.dem` + reglas, registra un handler para `events.Kill`, agrega kills por jugador objetivo en ventanas temporales, y emite el `kill plan`.

```go
package main

import (
    "log"

    demoinfocs "github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs"
    events "github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/events"
)

func main() {
    err := demoinfocs.ParseFile("/path/to/demo.dem", func(p demoinfocs.Parser) error {
        p.RegisterEventHandler(func(e events.Kill) {
            gs := p.GameState()
            // e.Killer, e.Victim, e.Weapon, e.IsHeadshot, e.PenetratedObjects
            // p.CurrentFrame(), gs.IngameTick(), gs.TotalRoundsPlayed()
            if e.Killer != nil && e.Killer.SteamID64 == targetSteamID {
                appendKill(gs.IngameTick(), e)
            }
        })
        p.RegisterEventHandler(func(e events.RoundEnd) {
            flushPendingSegment()
        })
        return nil
    })
    if err != nil { log.Fatal(err) }
}
```

## Datos que necesitamos de cada kill

Mapeo entre lo que ofrece la lib y lo que mete el `kill plan`:

| Campo en `kill plan`       | Origen en demoinfocs-golang                            |
|----------------------------|--------------------------------------------------------|
| `tick`                     | `parser.GameState().IngameTick()` en el handler        |
| `weapon`                   | `event.Weapon.Type` (enum `common.EquipmentType`)      |
| `headshot`                 | `event.IsHeadshot`                                     |
| `wallbang`                 | `event.PenetratedObjects > 0`                          |
| `killer_steamid64`         | `event.Killer.SteamID64`                               |
| `victim_steamid64`         | `event.Victim.SteamID64`                               |
| `killer_pos` / `victim_pos`| `event.Killer.Position()` / `event.Victim.Position()`  |
| `round`                    | `parser.GameState().TotalRoundsPlayed()`               |
| `tickrate`                 | `parser.TickRate()` (al inicio del parse)              |

## Agrupación en "segmentos"

Para evitar grabar 20 clips de 5 segundos por separado, agrupamos kills cercanas:
- Una "ventana" = kills del mismo jugador separadas por menos de `window_seconds` (config, default 10s).
- `tick_start` = tick de la primera kill − `pre_roll` (ej. 3 segundos de margen).
- `tick_end` = tick de la última kill + `post_roll` (ej. 5 segundos).
- Si una ronda termina dentro del segmento, el segmento se corta en el `events.RoundEnd`.

## Modo "mocking" para tests

La lib expone `pkg/demoinfocs/fake` con un parser fake que permite inyectar eventos y validar la lógica de agrupación sin necesidad de un `.dem` de verdad.

```go
parser := fake.NewParser()
parser.MockEvents(makeKill(common.EqAK47), makeKill(common.EqAWP))
parser.On("ParseToEnd").Return(nil)

kills := collectKills(parser)  // función bajo test
```

Esto es lo que vamos a usar para los unit tests del Demo Parser Service.
