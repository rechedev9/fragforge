# Referencia congelada: instalador de Windows

Fuente: `desktop/dist-installer/` y `desktop/package.json`.
Capturado: 2026-07-03.
Regla: la ejecución usa esta captura como canónica; no se rehace el build del instalador.

- Fichero: `desktop\dist-installer\FragForge Studio Setup 0.2.7.exe`
- Tamaño exacto: 130.050.278 bytes (~124 MB)
- Versión: 0.2.7 (desktop/package.json), productName "FragForge Studio", appId com.fragforge.studio
- Tipo: NSIS (electron-builder), oneClick:false, per-user, directorio elegible
- SIN FIRMAR: SmartScreen mostrará "Windows protected your PC / unknown publisher"; el usuario debe pulsar "More info" -> "Run anyway". La landing lo dice honestamente.
- Requisitos reales: Windows 10/11 x64; para capturar clips hace falta CS2 + HLAE + GPU (la landing puede resumirlo en requisitos).
- Destino de publicación: GitHub Release `v0.2.7` en rechedev9/fragforge (repo público), asset con el .exe.
- URL esperada del asset (verificar con `gh release view`): `https://github.com/rechedev9/fragforge/releases/download/v0.2.7/FragForge.Studio.Setup.0.2.7.exe` (GitHub normaliza espacios del nombre a puntos; usar la URL que devuelva la API, no esta suposición).
