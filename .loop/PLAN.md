# PLAN - Landing de descarga de FragForge Studio en Vercel

Generado desde SPEC.md el 2026-07-03; revisado tras la auditoría red-team (8 hallazgos aplicados, ver JOURNAL).
Modo: ejecución directa en sesión (no /loop programado): Fable orquesta, verifica y juzga; subagentes Opus implementan.
En este modo se permiten varias fases por pasada, con una entrada de JOURNAL.md por pasada.
El agente puede reordenar o partir fases dejando constancia en JOURNAL.md, pero nunca borrar ni debilitar un criterio de salida (eso va a BLOCKED.md).
Los hechos de reconocimiento de SPEC "Technical constraints" son observaciones fechadas, no requisitos: si la realidad difiere, se corrige en JOURNAL Current state; solo las contradicciones de Goal/Scope/Business rules van a BLOCKED.md.

## Stop values

- MAX iteraciones (pasadas de trabajo): 25
- Límite de no-progreso: 3 consecutivas
- Progreso = un criterio de salida de fase pasa de rojo a verde (con la salida real del verificador pegada en JOURNAL) o una entrada nueva en BLOCKED.md; todo lo demás (refactors, docs, journal) es no-progreso.
- Presupuesto de tiempo: 12 h desde Launched; la primera pasada escribe Launched con timestamp ISO (única edición permitida de PLAN.md).
- Token de finalización literal: `LOOP-COMPLETE-fragforge-landing`; solo se imprime inmediatamente después de pegar en JOURNAL la salida verde del verificador completo de esa misma pasada.
- Launched: 2026-07-03T01:05:00+02:00

## Tiers del verificador

- Rápido (cada pasada): `cd landing && npx tsc --noEmit && npm run build`
- Completo (fase F6 y pasada de cierre): tier rápido + `cd landing && npm run test:e2e` (Playwright: smoke + enlace de descarga + consola limpia) + verificación HTTP del asset de la Release + verificación de la URL de producción de Vercel.
- Ningún check se elimina, se salta ni se relaja; cada check nuevo se demuestra en rojo con un caso malo conocido antes de confiar en él, y la demostración (diff + salida roja) se apunta en JOURNAL.md.
- Prueba de mutación en el cierre: la pasada de cierre re-planta un `console.error` en la página y un href de CTA roto (en cambios temporales sin commitear) y confirma que la suite e2e ACTUAL falla por ambos; el diff exacto y la salida roja se pegan en JOURNAL. Un verificador que no se puede volver a poner en rojo cuenta como verificador roto y detiene el cierre (BLOCKED).
- Fallo ambiental: si un check falla por causa ambiental demostrada (p.ej. WebGL no disponible en Chromium headless), no se relaja el check: se fija el entorno (p.ej. lanzar con `--use-angle=default`, headed, o flags de GPU por software) y se registra en JOURNAL; si no se puede fijar en 1 pasada, va a BLOCKED como fallo de entorno, no de código.

## Fases

### F0 - GitHub Release v0.2.7 con el instalador

Trabajo: `gh release create v0.2.7 --target <SHA de origin/main> --title "FragForge Studio 0.2.7" --notes "<1-3 líneas: qué es, requisito Windows 10/11 x64, aviso SmartScreen (instalador sin firmar)>"` subiendo `desktop\dist-installer\FragForge Studio Setup 0.2.7.exe` como asset.
Idempotencia: si la release ya existe con el asset ausente o de tamaño incorrecto, usar `gh release upload v0.2.7 <exe> --clobber`; nunca borrar la release ni el tag, propios o ajenos.
Criterio de salida: `gh release view v0.2.7 --json assets` lista el asset y `curl -sIL <browser_download_url>` devuelve 200 con `content-length: 130050278`.

### F1 - Scaffold de landing/

Trabajo: app Next.js 15 + React 19 + TS + Tailwind 4 en `landing/`, con deps three/@react-three/fiber/drei/postprocessing, fuentes y tokens de `.loop/reference/brand.md`, página única con placeholder, y Playwright configurado (`npm run test:e2e`) con un smoke test que carga `/` y falla si hay errores de consola.
Criterio de salida: `cd landing && npx tsc --noEmit && npm run build` sale 0, y el smoke de Playwright pasa contra el server local; el check de consola se demuestra antes en rojo con un `console.error` plantado y la salida roja pegada en JOURNAL antes de retirarlo.

### F2 - Hero 3D "forja de partículas"

Trabajo: escena three.js/R3F a pantalla del hero: miles de partículas acid-lime tipo chispas de forja (shader de puntos o instancias), bloom (postprocessing), movimiento reactivo al puntero y al scroll, fondo charcoal, DPR limitado en móvil, fallback estático con `prefers-reduced-motion`, y pausa cuando la pestaña no es visible.
Criterio de salida: tier rápido verde + Playwright: el canvas WebGL existe y tiene tamaño > 0, cero errores de consola en 5 s de carga; con `prefers-reduced-motion: reduce` emulado la página renderiza el hero con fallback estático (sin canvas animado); en viewport 390x844 el DPR efectivo del canvas es <= 2.

### F3 - Landing completa (copy EN + descarga real)

Trabajo: hero con wordmark, titular y CTA "Download for Windows" (versión 0.2.7 y ~124 MB visibles) enlazando la URL real del asset de F0 (la `browser_download_url` devuelta por gh, nunca construida a mano); secciones qué-es, cómo-funciona (pasos), requisitos, nota SmartScreen honesta, footer con enlace al repo GitHub; metadata SEO + OG + favicon.
Criterio de salida: tier rápido verde + Playwright: el href del CTA coincide con la `browser_download_url` de la Release y un HEAD a ese href devuelve 200 (siguiendo redirecciones); existen las secciones (headings accesibles); cero errores de consola.

### F4 - Juicio visual independiente (bar "increíble/profesional")

Trabajo: script de captura versionado en `landing/e2e/screenshots.ts`, comando único que produce PNGs en 1440x900 y 390x844, arriba del todo y una captura por sección.
Protocolo de juez: el juez es Fable (agente distinto del ejecutor Opus); el juez ejecuta él mismo el script de captura y verifica que cada PNG tiene exactamente el viewport declarado y que hay una captura por sección; el ejecutor no filtra, resume ni selecciona lo que ve el juez; nombres de fichero en JOURNAL; cada ronda se registra etiquetada NON-DETERMINISTIC; la auto-revisión del ejecutor nunca cuenta.
Criterios verbatim que el juez aplica (todos deben ser SÍ):
1. Jerarquía visual clara: titular legible sobre la animación en < 1 s de vistazo.
2. La animación 3D se percibe intencional y premium, no un demo de three.js por defecto.
3. Consistencia de marca: lime + charcoal + tipografías correctas, sin colores fuera de paleta.
4. CTA de descarga dominante y con versión/tamaño visibles above-the-fold en desktop.
5. Espaciado y alineación sin roturas en 1440x900 ni en 390x844 (sin overflow horizontal, sin texto cortado).
6. Nada parece plantilla sin estilo (sin serif por defecto, sin bordes/botones de user-agent).
Criterio de salida: veredicto PASS del juez; máximo 3 rondas juez-arregla-juez.
Si a la 3a ronda no hay PASS: se registra en BLOCKED.md como decisión de calidad para Luis, F5 se abre como PR draft y F6 se detiene antes de `vercel --prod` hasta decisión de Luis.

### F5 - Publicación en GitHub

Trabajo: commit de `landing/` + `.loop/` en la rama `landing-vercel` (ya creada desde origin/main), push a origin, PR a main con descripción y evidencia del verificador pegada.
Criterio de salida: `gh pr view --json url,state` devuelve el PR abierto contra main y la rama remota existe.
El PR se queda abierto: main solo cambia cuando Luis lo aprueba y mergea; la pasada de cierre NUNCA lo mergea.

### F6 - Proyecto en Vercel con la CLI y deploy de producción

Trabajo: comprobar sesión de forma no interactiva (`vercel whoami` con timeout; ya autenticado como rechedev9 el 2026-07-03); desde `landing/`: `vercel link --yes` para crear el proyecto `fragforge-landing` y `vercel --prod` para el deploy.
Root Directory: tras el link, confirmar/fijar explícitamente `rootDirectory: landing` en la config del proyecto (vercel.json del proyecto o API de proyectos) para que un futuro git-connect nunca construya la raíz del monorepo; confirmar que el proyecto no tiene integración git activa, o si la tiene, que production branch + rootDirectory apuntan a landing.
Criterio de salida (verificador completo): `vercel ls` muestra el proyecto; la inspección del proyecto confirma `rootDirectory: landing` (salida pegada en JOURNAL); `curl -sI <url-prod>` devuelve 200; Playwright contra la URL de producción pasa el smoke (canvas presente, CTA con href de la Release, cero errores de consola).

## Límites de seguridad (prohibiciones comprobables)

- Nunca push directo a main; main solo cambia cuando Luis aprueba y mergea el PR de F5; la pasada de cierre deja el PR abierto con la evidencia y NUNCA lo mergea.
- Ningún fichero se crea/edita/borra fuera de `landing/`, `.loop/` y (solo F5) la rama git; prohibido tocar web/, desktop/, internal/, cmd/, effects/, overlays/, services/, bin/, data/, go.mod, package.json raíz.
- La pasada de cierre audita `git diff --stat origin/main...HEAD` contra esta lista como backstop.
- Dependencias npm permitidas en landing/: next, react, react-dom, three, @react-three/fiber, @react-three/drei, @react-three/postprocessing, tailwindcss, @tailwindcss/postcss, typescript, tipos @types/*, lucide-react, y @playwright/test como dev. Cualquier otra requiere entrada en BLOCKED.md.
- Vercel: solo `login`, `link`, `deploy/--prod`, `ls`, `inspect`/API de proyectos sobre `fragforge-landing`; prohibido `vercel remove`, `vercel env` con secretos, o tocar cualquier otro proyecto de la cuenta.
- GitHub: solo la Release v0.2.7 (crear/subir asset, nunca borrar release ni tag), la rama landing-vercel y su PR; prohibido borrar releases/tags/ramas o cambiar settings del repo.
- No se commitea ningún binario ni artefacto generado (el .exe va a la Release, jamás a git; screenshots de e2e quedan gitignorados).
- No se leen .env ni credenciales; el token de Vercel lo gestiona la CLI.

## Referencias congeladas

La ejecución usa `.loop/reference/brand.md` y `.loop/reference/installer.md` como canónicos; no se re-visita `web/` para re-derivar marca ni se rehace el instalador.
