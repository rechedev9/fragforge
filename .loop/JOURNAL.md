# JOURNAL

## Current state

Iteración 1 de 25 (en curso).
Modo: ejecución directa en sesión; Fable orquesta/verifica/juzga, subagentes Opus implementan.
Luis durmiendo desde ~01:00; orden explícita: "continúa con todo". El PR de F5 se deja abierto (nunca auto-merge).
Rama: landing-vercel (creada desde origin/main = 5c1e82c; refactorwindows ya estaba mergeado, 0 commits de diferencia).
Vercel CLI autenticada como rechedev9 (device flow completado por Luis 2026-07-03 ~00:55); gh autenticado; repo público.
Baseline del verificador: no existe baseline; `landing/` no existe todavía, construir el verificador es trabajo de F1 (regla cero, fallback registrado).
Fases: F0 OK, F1 OK, F2 OK; F3 en curso; F4..F6 pendientes.
URL canónica del instalador (de gh, para F3): https://github.com/rechedev9/fragforge/releases/download/v0.2.7/FragForge.Studio.Setup.0.2.7.exe
Entrevista: Luis AFK a los 60 s; decisiones (auto) = opciones Recommended, ver SPEC.md.

## Learnings

- El nombre del asset en GitHub Release normaliza espacios; usar siempre la browser_download_url devuelta por `gh release view --json assets`, nunca construirla a mano.
- `vercel whoami`/`vercel login` sin sesión arranca un device-login interactivo que cuelga la shell; lanzar comandos de vercel con timeout o en background.
- Stack de referencia ya probado en web/: Next 15 + React 19 + Tailwind 4 + three 0.184 + R3F 9 + drei 10 + postprocessing 3.
- Los Bash a background: el path del output file que devuelve el harness hay que copiarlo exacto (Glob si se pierde).

## Log

## Iteración 1 de 25
Done: recon, entrevista (AFK -> defaults Recommended), SPEC/PLAN/BLOCKED/references escritos, rama landing-vercel desde origin/main, auditoría red-team (agente a302902429d6cde09, 8 hallazgos) aplicada como reescritura única de PLAN.md: sin auto-merge a main, F0 idempotente con --target y --clobber, hechos de recon = observaciones fechadas, prueba de mutación en el cierre, juez ejecuta las capturas, definición de progreso + Launched ISO, verificación explícita de rootDirectory en F6, protocolo de fallo ambiental WebGL + DPR móvil <= 2 en F2.
Verifier: n/a (sin baseline; landing/ no existe, verificador es trabajo de F1).
Progress: sí (blueprint completo y auditado; streak 0 de 3).
Discovered: vercel login ya completado por Luis (SPEC Technical constraints queda desactualizado en ese hecho; corregido aquí, no en SPEC, según regla del PLAN).
Blocked: ninguno.
Next: F0 (Release v0.2.7) en background + F1 (scaffold landing/) con agente Opus.

## Iteración 2 de 25
Done: F0 y F1.
Verifier F0: `curl -sIL <browser_download_url>` -> final_status=200, Content-Length: 130050278 (exacto); asset FragForge.Studio.Setup.0.2.7.exe listado por gh.
Verifier F1 (agente Opus a269e4cd1bb530583): TSC_EXIT=0; BUILD_EXIT=0 (First Load JS 103 kB); e2e rojo demostrado con console.error plantado ("planted-red-demo", 1 failed) y verde tras retirarlo (1 passed).
Progress: sí (F0 y F1 rojo->verde; streak 0 de 3).
Discovered: npm audit reporta 2 vulnerabilidades moderadas en deps transitivas; no se ejecuta audit fix (fuera de la allowlist de deps). Queda anotado para Luis.
Blocked: ninguno.
Next: F2 (hero forja de partículas) con agente Opus.

## Iteración 3 de 25
Done: F2 (agente Opus a8c87bd183c014751): components/hero-forge.tsx + forge-canvas.tsx (GLSL propio: emisión reciclada desde bajo el fold, vórtice + simplex, ramp lime->blanco, sprites redondos aditivos, plano de brillo "forja" bajo-centro), bloom, parallax puntero/scroll, tiempo solo acumula con pestaña visible, DPR [1,2] desktop / [1,1.5] móvil, fallback estático reduced-motion, scrim de legibilidad.
Verifier: TSC_EXIT=0; BUILD_EXIT=0 (First Load / = 104 kB, three code-split); e2e 4 passed (29.3s). Red demos: canvas ausente (Timeout waiting #hero canvas), reduced-motion ignorado (toHaveCount(0) -> 1), DPR 3 (<=2 -> 3). WebGL headless OK vía SwiftShader sin flags.
Progress: sí (F2 rojo->verde con 3 checks nuevos demostrados; streak 0 de 3).
Discovered: ninguno.
Blocked: ninguno.
Next: F3 (landing completa, copy EN, CTA con URL real de la Release) con agente Opus.

## Iteración 4 de 25
Done: F3 (agente Opus aa57b367e8818f8f9): página completa (hero CTA con URL canónica, what-it-does, how-it-works, requirements, nota SmartScreen, footer), SEO + icon.svg + opengraph-image.tsx; e2e 7 passed con 3 red demos (href equivocado -> equality y 404; heading renombrado -> not found). F4 ronda 1 de juez [NON-DETERMINISTIC]: juez Fable ejecutó él mismo el script (e2e/screenshots.ts + screenshots.config.ts, 14 PNGs 1440x900 y 390x844) y leyó los PNGs directamente.
Verifier: e2e 7 passed (33.0s); capturas 2 passed (1.3m).
Veredicto ronda 1: FAIL. Criterio 2 NO (el hero se ve como blobs borrosos olive + columna de neblina gris quemada centro-pantalla; no chispas nítidas), criterio 3 NO en el hero (neblina gris/blanca fuera de paleta; el resto de la página SÍ), criterio 4 dudoso (microcopy v0.2.7 · 124 MB casi ilegible sobre la neblina). Criterios 1, 5, 6 SÍ (headline legible, sin roturas 1440/390, nada de plantilla; secciones de contenido excelentes).
Progress: sí (F3 rojo->verde; F4 en ronda 1 de 3; streak 0 de 3).
Discovered: screenshots.ts necesitó config propia (testMatch por defecto solo coge *.spec.ts); footer sin id, se captura por selector footer.
Blocked: ninguno.
Next: ronda 2 - el ejecutor F2 (a8c87bd183c014751) corrige el hero: puntos pequeños y nítidos, glow plane mínimo y pegado abajo, bloom solo en núcleos calientes, fondo charcoal casi negro, microcopy legible; recaptura y re-juicio.

## Iteración 5 de 25
Done: F4 ronda 2 [NON-DETERMINISTIC]. Ejecutor corrigió: size attenuation 300->12/-mv.z, clamp 70->9*uPixelRatio, uSize 26->3, falloff pow(1-d,2.2) sin halo gaussiano, glow plane 30x22 y=-3.4 -> 26x8 y=-6.2 (pico bajo el fold), bloom intensity 0.9->0.5 threshold 0.22->0.65 radius 0.7->0.55, partículas 9000->14000 desktop / 7000 móvil. E2E 7 passed (30.2s), capturas regeneradas (2 passed, 44.7s).
Verifier: E2E_EXIT=0 (7 passed); juez Fable releyó desktop-top.png y mobile-top.png directamente.
Veredicto ronda 2: PASS (criterios 1-6 todos SÍ: charcoal casi negro, miles de chispas nítidas, glow confinado abajo, headline/CTA dominantes, microcopy legible, móvil limpio).
Progress: sí (F4 rojo->verde; streak 0 de 3).
Discovered: ninguno.
Blocked: ninguno.
Next: checkpoint git + F5 (push + PR) + F6 (Vercel CLI).
