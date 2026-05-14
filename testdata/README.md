# testdata/

Demos `.dem` y planes esperados (`*.expected.json`) usados por los golden tests del
demo parser. Los archivos `.dem` y `.expected.json` NO se commitean al repo —
cada desarrollador aporta los suyos localmente.

## Convención de nombres

```
testdata/
├── <slug>.dem                 # demo CS2 (no committed)
├── <slug>.expected.json       # plan esperado (no committed)
└── <slug>.rules.json          # opcional: reglas usadas para generarlo (committed si es un golden de referencia)
```

## Cómo generar un golden manualmente

```bash
zv-parser parse \
  --demo testdata/<slug>.dem \
  --steamid <SteamID64> \
  --rules testdata/<slug>.rules.json \
  --out testdata/<slug>.expected.json
```

Una vez confirmado a ojo que el plan es razonable, vuelve a ejecutar la prueba:
debe pasar bit-a-bit contra ese archivo.

## Fuentes de demos públicas

- HLTV: https://www.hltv.org — buscar partido y descargar el `.dem`
- Faceit: las demos de tus partidas están en tu perfil

Para tener al menos un demo de referencia recomendamos descargar uno corto (mapa
único, ~30 min) de algún torneo reciente y dejarlo en `testdata/`.
