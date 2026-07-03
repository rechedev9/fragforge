# Referencia congelada: marca FragForge

Fuente: `web/app/globals.css`, `web/components/brand/wordmark.tsx`, `web/app/layout.tsx`.
Capturado: 2026-07-03.
Regla: la ejecución usa esta captura como canónica para la landing; no se re-deriva de `web/` a mitad de loop.

## Tokens de color (oklch)

- background: `oklch(0.145 0.006 264)` (charcoal frío desaturado)
- foreground: `oklch(0.985 0 0)`
- card: `oklch(0.196 0.0065 264)`
- primary (acid-lime): `oklch(0.905 0.182 124)`
- primary-foreground: `oklch(0.205 0.03 124)`
- muted-foreground: `oklch(0.66 0.01 264)`
- border: `oklch(0.275 0.006 264)`
- destructive (rojo): `oklch(0.62 0.21 25)`
- radius base: `0.75rem`

Tema forzado oscuro; no hay modo claro.

## Wordmark

`Frag` en primary (lime) + `Forge` en foreground (blanco), font display, bold, tracking-tight.
Marca opcional: cuadrado redondeado bg-primary con icono Flame (lucide) en primary-foreground.

## Tipografía

- Display: Space Grotesk (`--font-space-grotesk`)
- Texto: Inter (`--font-inter`)
- Mono/datos: JetBrains Mono (`--font-jetbrains-mono`)
Cargadas con `next/font/google`.

## Voz

Producto: FragForge (app de escritorio: "FragForge Studio").
Tono: "the replay studio"; señal ácida sobre fondo sobrio; sin gradientes multicolor genéricos.
