# FragForge Studio — design system v3

This file is the presentation contract for `web/`. The typed API layer,
polling, local/cloud routing, and the demo-to-render state machines are not
part of the visual system and must remain stable during UI work.

## Product idea

FragForge is a local-first replay workstation: a focused CS2 production tool,
not a generic SaaS dashboard and not a decorative cyberpunk HUD. The interface
should feel like a calm broadcast control room: technical, dense where data is
useful, and quiet around the current action.

The visual identity is:

- blue-black canvas and layered navy surfaces;
- cyan for primary action, selection, navigation, and keyboard focus;
- vermilion for the wordmark micro-accent; magenta remains reserved for
  stream-specific signals such as REC, facecam, music, and likes;
- mint for ready/success, amber for warnings/expiry, red for failures and
  destructive actions;
- angular geometry, thin borders, restrained corner cuts, and minimal glow;
- subtle grid and scanline texture on the canvas only.

## Bottom-up hierarchy

Visual changes are made in this order:

1. tokens in `app/globals.css`;
2. primitives in `components/ui`;
3. Studio patterns in `components/studio`;
4. domain components (`matches`, `upload`, `videos`, `feed`, `streams`);
5. pages and responsive composition.

Pages should not duplicate title blocks, empty-state layouts, button geometry,
input focus rules, or filter-chip styling when a shared component exists.

## Foundations

### Color roles

The CSS variables in `app/globals.css` are the source of truth.

| Role | Token | Use |
| --- | --- | --- |
| Canvas | `--background` | app background |
| Panel | `--surface` / `--card` | standard surfaces |
| Raised panel | `--surface-raised` | selected or important surfaces |
| Text | `--foreground` | headings and essential content |
| Secondary text | `--muted-foreground` | descriptions and metadata |
| Primary signal | `--primary` | CTA, active nav, focus, selection |
| Stream signal | `--stream` | REC, stream, facecam, music, likes |
| Ready | `--success` | completed and available |
| Warning | `--warning` | expiry and recoverable warning |
| Danger | `--destructive` | error, failure, delete |

Do not use magenta as a generic error colour or as the active navigation colour.
Do not rely on colour alone to communicate state.

### Type

- Display/UI: Chakra Petch (`--font-display`, `--font-sans`).
- Operational metadata and numbers: Share Tech Mono (`--font-mono`) with
  tabular figures.
- Desktop H1: 40–42px; mobile H1: 30–32px.
- Standard body copy: 15px/24px.
- Labels: at least 13px; essential text never below 12px.
- Wide tracking and full uppercase are reserved for eyebrows, states, and
  compact metadata—not paragraphs.

### Space and controls

Use the scale `4, 8, 12, 16, 24, 32, 48, 64, 96`.

- Default button/input: 44px high.
- Small button/icon control: at least 40px high.
- Panel padding: 20–24px mobile, 24–32px desktop.
- Main content gap: 28–40px.
- Sidebar row: 48px.
- Focus: visible 2px cyan ring with a dark offset.

### Surfaces and texture

`studio-panel` is the normal content surface. `studio-panel-raised` adds visual
priority, and `studio-panel-interactive` supplies restrained hover elevation.
The background grid and scanlines remain low-opacity and must never reduce text
contrast. Use one dominant angular treatment per component: a notch, brackets,
or an accented border—not all three.

## Shared patterns

### `StudioPageHeader`

Every Studio page uses the same eyebrow, title, description, and optional
action area. Title and content share the same left edge.

### `StudioEmptyState`

Empty states are bounded, actionable panels with an icon, H2, concise copy,
one primary action, an optional secondary action, and an optional trust/status
line. They should normally remain between 640px and 760px wide instead of
stretching across the full workspace.

### Buttons and fields

Primary buttons use cyan. Stream import actions may use magenta when their
category is already clear. Destructive actions use red. Inputs use solid-enough
navy surfaces, a visible border, a real label, and inline validation.

### Filters

Segmented controls have 40px minimum height, `aria-label`/`aria-pressed`
semantics from Radix, and cyan active state. On narrow screens they may scroll
horizontally, but must not shrink labels below readability.

### Status

Queued, recording, composing, ready, failed, and expiry states always include
text or an icon as well as colour. Pipeline information belongs inside the
relevant card or section, never floating in unused page space.

## Shell

- Desktop sidebar: 240px; collapses to the existing icon rail.
- Main content: maximum 1440px with fluid 24–48px horizontal padding.
- Active navigation is always cyan. Stream retains a small magenta category
  marker.
- Creation destinations and content destinations are separated by spacing.
- Capture readiness is a fully clickable operational status with a readable
  icon, label, hint, keyboard focus, and diagnostic dialog.
- Mobile retains the existing sheet navigation and 56px top bar.

`/upload` remains outside the authenticated route group because it supports the
no-login flow. It uses a compact standalone top bar and the same content tokens,
widths, typography, and controls as the Studio shell.

## Screen contracts

### Matches

The page is the Studio inbox. Empty state routes to demo upload and stream
import. Populated rows are dense scoreboard surfaces: map/context, score,
K/D/A/MVP/KD, timestamp/highlight count, and a clear 44px action. Search and
filters remain usable on mobile.

### Upload

Keep the real `scan -> player picker -> parse` flow. The dropzone is one large
keyboard-operable target with clear drag, focus, error, scanning, picking,
offline, and parsing states. State that processing is local in readable text.

### Stream clips

The source panel uses a neutral surface; magenta identifies the stream action,
not the whole background. URL and MP4 paths remain equally discoverable. On
wide screens, use otherwise-empty space for a small output/processing summary.
Do not mix visual refactoring with changes to acquisition or render state.

### Library

Use an auto-fill grid with cards around 260–300px wide. A reel card owns its
thumbnail, format, title, pipeline/status, expiry, and actions. Preserve real
media URLs, publishing, deletion, and polling semantics.

### Feed

Use a responsive card grid with clear play and like targets. Empty state routes
to the Library and upload flow. Community/like details may use magenta; global
selection remains cyan.

## Responsive and accessibility

Validate at 390, 768, 1024, 1440, and 1920px and at 200% zoom.

- Below 768px, forms and paired actions stack; filters may scroll.
- Cards collapse to one column without horizontal overflow.
- Interactive targets are at least 40px and normally 44px.
- Standard text contrast is at least 4.5:1; controls/focus at least 3:1.
- Use `aria-current` for navigation, `aria-live` or `role=alert` for async
  status/errors, and accessible names for icon actions.
- Respect `prefers-reduced-motion` and `forced-colors`.
- Never hide essential controls behind hover only; keyboard focus must reveal
  equivalent actions.

## Functional invariants

- Keep `web/lib/api/*`, API field names, object URLs, polling, and local/cloud
  routing stable unless the task explicitly changes behaviour.
- Preserve `/upload` file input and roster flow.
- Preserve accessible labels and existing E2E hooks such as `data-slot="card"`,
  `data-testid="player-avatar"`, download/delete labels, and sticky reel action.
- The match-detail highlight selector remains a vertical list; do not revive
  the obsolete horizontal filmstrip contract.
- Use real API data. Never fabricate progress, duration, output format, or
  media availability merely to fill a design.
