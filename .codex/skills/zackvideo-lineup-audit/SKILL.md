---
name: zackvideo-lineup-audit
description: "Review and correct ZackVideo CS2 utility destination labels using utility-audit CSV output and manual lineup catalog JSON files."
---

# ZackVideo Lineup Audit

Use this skill when utility clips have wrong smoke/flash/molotov destinations or the user asks to make destination detection deterministic.

## Process

1. Generate or refresh the audit:

```powershell
.\bin\zv.exe workflows run utility-audit -- `
  --plan <run>\plan-utility.json `
  --lineup-catalog data\lineups `
  --out <run>\utility-audit.csv
```

2. Inspect rows where `destination_source` is `auto` or `unknown`.

3. For wrong smoke destinations, add a manual catalog entry in `data\lineups\<map>.smokes.json`.

Use measured `landing_x`, `landing_y`, `landing_z` as the destination cluster center and measured `throw_x`, `throw_y`, `throw_z` when origin disambiguation is needed. Keep radii tight enough to avoid matching unrelated lineups.

4. Re-run the audit and confirm corrected rows now use `destination_source=catalog`.

5. If the destination appears in burned-in overlay text, rerender the videos without `--skip-existing`.

## Review Standard

- `catalog`: reviewed/manual enough to use in overlay text.
- `auto`: useful guess, not final truth.
- `unknown`: needs review before publishing.

Do not relabel a destination only from the throw location. The landing/pop/inferno center is the source of truth for destination.
