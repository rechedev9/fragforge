# SPEC - Landing de descarga de FragForge Studio en Vercel

Decisiones tomadas el 2026-07-03 durante la entrevista pre-loop.
Luis no respondió en la ventana de 60s, así que cada decisión marcada (auto) es la opción "(Recommended)" de la entrevista, elegida por el agente de diseño y revisable por Luis en cualquier momento.
Este fichero es de solo lectura para el agente durante la ejecución.
Una contradicción encontrada aquí a mitad de ejecución va a BLOCKED.md como pregunta de spec; nunca se edita este fichero para que encaje con el código.

## Goal

Una landing page profesional para descargar el instalador de Windows de FragForge Studio, desplegada en Vercel, con una animación 3D hero en three.js de nivel "wow".
Al final: código subido a GitHub (rechedev9/fragforge) y proyecto creado en Vercel con la CLI de Vercel.

## Scope (qué se construye)

- App Next.js mínima y nueva en `landing/` dentro de este repo (auto). Vercel se configura con Root Directory = `landing`, de modo que solo la landing se despliega, nunca la app usable de `web/`.
- Hero con animación 3D "forja de partículas" (auto): miles de partículas/chispas acid-lime sobre charcoal, con bloom, reactivas al ratón y al scroll, degradación elegante en móvil y con `prefers-reduced-motion`.
- Botón de descarga que apunta al asset real de una GitHub Release v0.2.7 del repo público (auto), mostrando versión (0.2.7) y tamaño (~124 MB) reales.
- Secciones: hero + CTA de descarga, qué hace el producto (demo .dem -> Short vertical listo para subir), cómo funciona (3-4 pasos), requisitos de sistema, nota honesta de instalador sin firmar (SmartScreen), footer.
- Copy en inglés (auto).
- SEO básico: title, description, Open Graph, favicon.
- Publicación: commit en rama dedicada, push a GitHub, PR a main, y despliegue de producción con `vercel` CLI.

## Business rules (hechos no inferibles del código)

- La app usable (web/) NO se despliega en Vercel; requisito explícito de Luis.
- El instalador existe ya en disco: `desktop\dist-installer\FragForge Studio Setup 0.2.7.exe`, 130.050.278 bytes, gitignorado. Es demasiado grande para servirlo desde Vercel; se sube como asset de GitHub Release.
- El repo rechedev9/fragforge es público, así que el asset de la Release se descarga sin login.
- El instalador está sin firmar: SmartScreen mostrará "unknown publisher". La landing debe mencionarlo honestamente (nota "More info -> Run anyway"), no ocultarlo.
- Diseño lo dirige Fable (este agente); la implementación la ejecutan subagentes Opus (instrucción explícita de Luis).
- Marca: FragForge (producto descargable: "FragForge Studio"). Acid-lime sobre charcoal oscuro; tokens exactos congelados en `.loop/reference/brand.md`.

## Technical constraints (del reconocimiento)

- Stack de la landing alineado con `web/`: Next.js 15 (App Router), React 19, Tailwind 4, TypeScript, three ^0.184 + @react-three/fiber ^9 + drei + postprocessing.
- Fuentes como en `web/`: Space Grotesk (display), Inter (texto), JetBrains Mono (datos), vía `next/font/google`.
- Node y npm ya instalados; Vercel CLI 52.0.0 instalada pero SIN sesión iniciada (el device-login queda pendiente de Luis); gh CLI autenticada como rechedev9.
- Sin config previa de Vercel en el repo (ni vercel.json ni .vercel).
- La rama de trabajo parte de origin/main para que el PR a main no arrastre commits de refactorwindows.

## Out of scope (no-goals explícitos)

- Desplegar `web/` (la app usable) o cualquier otra parte del monorepo en Vercel.
- Tocar código existente fuera de `landing/` y `.loop/` (nada en web/, desktop/, internal/, cmd/, effects/, overlays/, services/).
- Firmar el instalador, auto-update, o rehacer el build del instalador.
- CI/CD (GitHub Actions), analytics, cookies, formularios, newsletter, login.
- Versión multi-idioma o toggle de idioma.
- Descargas para macOS/Linux.
- Dominio custom en Vercel (se usa el dominio *.vercel.app generado).
- CMS o contenido editable; el copy vive en el código.
- Vídeo real del producto embebido (no hay asset apto; `web/public/sample-reel.mp4` es un placeholder de 114 KB y no se usa como demo del producto).
