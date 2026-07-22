# FACEIT Demo Index Guide

FragForge can turn a FACEIT player URL or nickname into a durable CS2 match
index. The index contains every match in the requested UTC date range,
match-level statistics, actual demo availability reported by FACEIT, canonical
room links, and a deterministic shortlist order for content review.

The FACEIT Data API key is read only from `FACEIT_API_KEY`. It is never accepted
as a command flag, printed, or persisted in the manifest.

## Preflight and indexing

Load the existing Windows user variable into the current PowerShell process
without printing it:

```powershell
$env:FACEIT_API_KEY = [Environment]::GetEnvironmentVariable("FACEIT_API_KEY", "User")
```

Discover and validate the command contract before network work:

```powershell
.\bin\zv.exe capabilities --format json
.\bin\zv.exe workflows show faceit-index --format json
.\bin\zv.exe workflows validate faceit-index --format json -- --profile https://www.faceit.com/en/players/m0NESY --from 2026-01-01 --to 2026-07-22 --out data\faceit\m0nesy-2026.json --dry-run --format json
.\bin\zv.exe workflows run faceit-index -- --profile https://www.faceit.com/en/players/m0NESY --from 2026-01-01 --to 2026-07-22 --out data\faceit\m0nesy-2026.json --dry-run --format json
```

The preflight performs no network request and does not create `--out`. Preserve
the approved argv and remove only `--dry-run` to create the index:

```powershell
.\bin\zv.exe workflows run faceit-index -- --profile https://www.faceit.com/en/players/m0NESY --from 2026-01-01 --to 2026-07-22 --out data\faceit\m0nesy-2026.json --format json
```

When `--from` and `--to` are omitted, the range defaults to January 1 through
today in UTC. The implementation partitions long ranges by month and paginates
FACEIT endpoints so a year is not silently truncated.

## Manual download stage

Until FACEIT approves Download API access:

1. Read `highlight_match_ids` in order, or inspect every entry in `matches`.
2. Open the matching `room_url` in the authenticated browser.
3. Use FACEIT's Watch/Demo download action.
4. Extract the resulting `.dem` if FACEIT supplied a compressed archive.
5. Keep the original downloaded archive until final media QA is complete.

`demo_resource_urls` records the private demo resources reported by the Data
API. It is evidence that a demo exists, not a promise that the unsigned URL is
directly downloadable. Automated signed downloads remain disabled until the
Download API application is approved and that separate operation is added.

## Continue into vertical production

FACEIT statistics only prioritize which match to inspect. They do not identify
AWP kills, POV ticks, camera ranges, or edit boundaries. After downloading a
demo, use the normal source-of-truth sequence:

```powershell
.\bin\zv.exe demo players --demo match.dem --format json --out data\runs\match\players.json
.\bin\zv.exe demo parse --demo match.dem --steamid <SteamID64> --out data\runs\match\plan.json
.\bin\zv.exe demo moments --killplan data\runs\match\plan.json --top 10 --out data\runs\match\moments.json --format json
.\bin\zv.exe demo select --killplan data\runs\match\plan.json --segments <ids> --out data\runs\match\selected-plan.json --dry-run --format json
```

Capture and vertical rendering still require the creative brief and thumbnail
approval gates described in `CLAUDE.md`. No HLAE, CS2, or FFmpeg work happens
during FACEIT indexing.
