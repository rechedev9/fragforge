# FragForge CLI command catalog

Canonical, machine-checked catalog of every executable `zv` command, workflow,
and skill exposed by the unified CLI. `zv check` and the `cmd/zv` end-to-end
doc tests verify this file stays aligned with the workflow catalog; keep every
cataloged workflow's direct command and `zv workflows run` form listed here.
For a curated introduction, read the repository [README](../../README.md).

## Direct commands

```bash
./bin/zv demo parse --demo testdata/foo.dem --steamid 76561198000000000 --out plan.json
./bin/zv demo players --demo testdata/foo.dem
./bin/zv utility audit --plan plan-utility.json --lineup-catalog data/lineups --out utility-audit.csv
./bin/zv record --killplan plan.json --demo testdata/foo.dem --out data/runs/run-004/recording --hlae C:\HLAE-2.190.1\HLAE.exe --cs2 "C:\Games\Counter-Strike 2\game\bin\win64\cs2.exe"
./bin/zv compose final --recording-result data/runs/run-004/recording/recording-result.json --out data/runs/run-004/final.mp4
./bin/zv music analyze --input data/music/track.mp4 --out data/runs/run-004/rhythm.json
./bin/zv shorts render --recording-result data/runs/run-004/recording/recording-result.json --out data/runs/run-004/shorts-natural-hq2-full --preset natural-hq2-full
./bin/zv analysis tactical-data --demo testdata/foo.dem --out data/runs/run-004/tactical.json --start 1000 --end 2000
./bin/zv analysis view --json data/analysis/MarcusN1-deaths.json
./bin/zv gallery open --path data/runs/run-004/shorts-natural-hq2-full/publish/index.html
./bin/zv check
./bin/zv check --format json
./bin/zv serve
./bin/zv pipeline --killplan plan.json --demo testdata/foo.dem --out data/runs/run-004/pipeline --hlae C:\HLAE-2.190.1\HLAE.exe --cs2 "C:\Games\Counter-Strike 2\game\bin\win64\cs2.exe"
```

## Skills

```bash
./bin/zv skills list
./bin/zv skills show zackvideo-cheater-pov-reels
./bin/zv skills show zackvideo-cs2-utility-shorts
./bin/zv skills show zackvideo-lineup-audit
./bin/zv skills show zackvideo-music-scripted-shorts
./bin/zv skills show zackvideo-shorts-production
./bin/zv skills show zackvideo-youtube-shorts-publish
./bin/zv skills check
./bin/zv skills list --format json
./bin/zv skills show zackvideo-cheater-pov-reels --format json
./bin/zv skills show zackvideo-cs2-utility-shorts --format json
./bin/zv skills show zackvideo-lineup-audit --format json
./bin/zv skills show zackvideo-music-scripted-shorts --format json
./bin/zv skills show zackvideo-shorts-production --format json
./bin/zv skills show zackvideo-youtube-shorts-publish --format json
./bin/zv skills check --format json
```

## Workflows

```bash
./bin/zv workflows list
./bin/zv workflows list --format json
./bin/zv workflows show demo-parse
./bin/zv workflows show demo-parse --format json
./bin/zv workflows show demo-players
./bin/zv workflows show demo-players --format json
./bin/zv workflows show utility-audit
./bin/zv workflows show utility-audit --format json
./bin/zv workflows show record
./bin/zv workflows show record --format json
./bin/zv workflows show compose-final
./bin/zv workflows show compose-final --format json
./bin/zv workflows show music-analyze
./bin/zv workflows show music-analyze --format json
./bin/zv workflows show shorts-render
./bin/zv workflows show shorts-render --format json
./bin/zv workflows show analysis-tactical-data
./bin/zv workflows show analysis-tactical-data --format json
./bin/zv workflows show analysis-viewer
./bin/zv workflows show analysis-viewer --format json
./bin/zv workflows show gallery-open
./bin/zv workflows show gallery-open --format json
./bin/zv workflows show serve
./bin/zv workflows show serve --format json
./bin/zv workflows show pipeline
./bin/zv workflows show pipeline --format json
./bin/zv workflows show skills-check
./bin/zv workflows show skills-check --format json
./bin/zv workflows show workflows-check
./bin/zv workflows show workflows-check --format json
./bin/zv workflows show project-check
./bin/zv workflows show project-check --format json
./bin/zv workflows run demo-parse -- --demo testdata/foo.dem --steamid 76561198000000000 --out plan.json
./bin/zv workflows run demo-players -- --demo testdata/foo.dem
./bin/zv workflows run utility-audit -- --plan plan-utility.json --lineup-catalog data/lineups --out utility-audit.csv
./bin/zv workflows run record -- --killplan plan.json --demo testdata/foo.dem --out data/runs/run-004/recording --hlae C:\HLAE-2.190.1\HLAE.exe --cs2 "C:\Games\Counter-Strike 2\game\bin\win64\cs2.exe"
./bin/zv workflows run compose-final -- --recording-result data/runs/run-004/recording/recording-result.json --out data/runs/run-004/final.mp4
./bin/zv workflows run music-analyze -- --input data/music/track.mp4 --out data/runs/run-004/rhythm.json
./bin/zv workflows run shorts-render -- --recording-result data/runs/run-004/recording/recording-result.json --out data/runs/run-004/shorts-natural-hq2-full --preset natural-hq2-full
./bin/zv workflows run analysis-tactical-data -- --demo testdata/foo.dem --out data/runs/run-004/tactical.json --start 1000 --end 2000
./bin/zv workflows run analysis-viewer -- --json data/analysis/MarcusN1-deaths.json
./bin/zv workflows run gallery-open -- --path data/runs/run-004/shorts-natural-hq2-full/publish/index.html
./bin/zv workflows run serve
./bin/zv workflows run pipeline -- --killplan plan.json --demo testdata/foo.dem --out data/runs/run-004/pipeline --hlae C:\HLAE-2.190.1\HLAE.exe --cs2 "C:\Games\Counter-Strike 2\game\bin\win64\cs2.exe"
./bin/zv workflows run skills-check
./bin/zv workflows run skills-check -- --format json
./bin/zv workflows run workflows-check
./bin/zv workflows run workflows-check -- --format json
./bin/zv workflows run project-check
./bin/zv workflows run project-check -- --format json
./bin/zv workflows check
./bin/zv workflows check --format json
```
