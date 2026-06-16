# FragForge — Web design system (v2)

This is the design contract for the FragForge web app under `web/`. It replaces
the v1 look (which was a near-copy of a competitor: dark + violet + top tabs).
v2 is a distinct, opinionated identity built on **shadcn/ui**.

The data layer does **not** change: the typed `ApiClient` + `MockApiClient` in
`web/lib/api/*` and `useSession()` stay exactly as they are. v2 is a
presentation-layer redesign only, so the Fase 2 swap to a real backend is
untouched.

---

## 1. Concept

**"The replay studio."** FragForge turns your own CS2 demos into highlight
reels, recorded on your own rig. The UI should feel like a focused creator/
editing tool (Linear / Vercel / a demo-review app), not a marketing SaaS
dashboard. Confident, fast, a little broadcast-room. We lean into the one thing
that is uniquely ours: a transparent **capture → edit → reel** pipeline that
runs on the player's PC.

**Two ways in.** (1) *Steam* — sign in, link your match history, and forge reels
from your own demos. (2) *Upload* — drop any `.dem` (yours or someone else's),
no login required, and analyze it the same way. Both converge on the identical
highlight → render pipeline; only the front door differs.

Differentiators from v1 (these are non-negotiable, they are what makes it "not a
copy"):

1. **Left sidebar IA**, not a centered 3-tab top bar.
2. **Acid-lime signal color** on desaturated charcoal — not violet.
3. **Monospace, tabular numbers** for every stat (scoreboard / demo-tick feel).
4. A **pipeline stepper** (Queued → Capturing → Editing → Ready) as a first-class
   component — our product story made visible.
5. A **filmstrip** play selector (horizontal tiles), not a vertical card list.
6. A **"LIVE ON YOUR RIG" REC** indicator tying renders to the user's machine.
7. Subtle **film grain** for a tape/replay texture.

Working brand name: **FragForge** (wordmark: lime "Frag" + white "Forge", with a
small spark/forge mark). Trivial to rename — it is one component.

---

## 2. Foundations

### Color (dark-first, shadcn tokens in `app/globals.css`)

Use shadcn's CSS-variable theming for Tailwind v4 (oklch). Base color on init =
`neutral`, then OVERRIDE the token values below. App is forced dark (`<html
class="dark">`); the `.dark` block is the source of truth.

```
/* .dark */
--background:        oklch(0.145 0.006 264);  /* deep cool charcoal */
--foreground:        oklch(0.985 0 0);
--card:              oklch(0.185 0.006 264);
--card-foreground:   oklch(0.985 0 0);
--popover:           oklch(0.185 0.006 264);
--popover-foreground:oklch(0.985 0 0);
--primary:           oklch(0.905 0.182 124);  /* ACID LIME — signal color */
--primary-foreground:oklch(0.205 0.03 124);   /* near-black text on lime */
--secondary:         oklch(0.245 0.006 264);
--secondary-foreground: oklch(0.985 0 0);
--muted:             oklch(0.245 0.006 264);
--muted-foreground:  oklch(0.66 0.01 264);
--accent:            oklch(0.27 0.008 264);
--accent-foreground: oklch(0.985 0 0);
--destructive:       oklch(0.62 0.21 25);      /* red — REC dot / destructive */
--border:            oklch(0.275 0.006 264);
--input:             oklch(0.275 0.006 264);
--ring:              oklch(0.905 0.182 124);   /* lime focus ring */
--radius:            0.75rem;
/* sidebar */
--sidebar:               oklch(0.13 0.006 264); /* a touch darker than bg */
--sidebar-foreground:    oklch(0.82 0.01 264);
--sidebar-primary:       oklch(0.905 0.182 124);
--sidebar-primary-foreground: oklch(0.205 0.03 124);
--sidebar-accent:        oklch(0.22 0.006 264);
--sidebar-accent-foreground: oklch(0.985 0 0);
--sidebar-border:        oklch(0.24 0.006 264);
--sidebar-ring:          oklch(0.905 0.182 124);
```

**Lime discipline:** lime is a *signal*, used sparingly — primary CTA, active
nav item, brand mark, focus ring, the active/done pipeline step, win bars, and
selection rings. Everything else is neutral charcoal + zinc text. Overusing lime
reads cheap; restraint reads premium. Red is reserved for the live REC dot and
destructive actions. Win = lime, loss = muted zinc (not red).

### Type (via `next/font/google`)

- **Display** — `Space Grotesk` (`--font-display`): all big headings, hero,
  screen H1s, the wordmark. Used uppercase with tight tracking for section
  eyebrows.
- **Body/UI** — `Inter` (`--font-sans`): default text, buttons, labels.
- **Mono/numbers** — `JetBrains Mono` (`--font-mono`): EVERY stat, score, K/D,
  tick, duration, countdown, pairing code. Always `tabular-nums`.

Map `--font-sans`/`--font-mono` into the shadcn `@theme inline` block and add
`--font-display`. Headings use `font-[family-name:var(--font-display)]`.

### Space, radius, motion

- Generous whitespace; max content width ~1200px; comfortable line-height.
- Radius 0.75rem base; cards `rounded-xl`; pills/badges `rounded-full`.
- Borders are 1px hairlines (`--border`), never heavy.
- Motion: fast (150–200ms), ease-out. Rendering cards get a subtle shimmer.
  Honor `prefers-reduced-motion` (disable shimmer/pulse).

### Texture

- A global, very low-opacity **film-grain** overlay (`GrainOverlay`, fixed,
  `pointer-events-none`, ~3–5% opacity SVG/feTurbulence). Subtle.
- Auth hero only: a faint scanline/vignette gradient on top of the grain.

---

## 3. Navigation / shell

A persistent **left sidebar** (shadcn `Sidebar`), collapsible, mobile → `Sheet`:

- Top: FragForge wordmark + spark mark.
- Items: **Matches** (target icon), **Upload** (upload-cloud icon), **Library**
  (film icon), **Feed** (compass/flame icon). Active item = lime text + lime left
  accent.
- Footer: a **slots meter** (`used / total`, a thin lime progress) with a small
  "Get more" button, then the user (avatar + persona, dropdown → sign out).

Onboarding screens (`/` auth, `/connect`) render WITHOUT the sidebar (root
layout only) — you are not "in the app" yet. Everything else lives under the
`(app)` route group whose layout renders the sidebar shell.

---

## 4. Signature components (shared, built in Foundation)

- **`StatMono`** — a labeled mono number (muted label above/inline, value in
  JetBrains Mono `tabular-nums`). Used for K / D / A / MVP / K/D / scores / ticks.
- **`ScoreBar`** — thin vertical accent bar on match rows: lime = win, zinc =
  loss. Quick visual scan.
- **`PipelineSteps`** — horizontal stepper: `Queued → Capturing → Editing →
  Ready`. Done steps lime-filled, active step pulses lime, future steps muted.
  Maps from `VideoStatus` (queued→Queued, recording→Capturing, composing→Editing,
  ready→Ready). This is the hero of the product story.
- **`RecDot`** — small pulsing red dot + optional "LIVE ON YOUR RIG" label, for
  videos in `recording` state.
- **`SectionEyebrow`** — uppercase Space Grotesk label + count, for section heads.
- **`GrainOverlay`** — the global texture layer.
- **`Filmstrip`** — horizontal `ScrollArea` row of selectable play tiles.

Build the rest from shadcn primitives directly.

### shadcn components to install

`sidebar, button, card, dialog, badge, input, label, avatar, progress, tabs,
tooltip, scroll-area, separator, skeleton, dropdown-menu, sheet, sonner,
toggle-group`. Wire `<Toaster />` (sonner) into the root layout.

---

## 5. Screens

All screens keep the SAME `api` calls and flow as v1; only the look/layout
changes.

### `/` — Auth
Full-bleed dark hero with grain + scanline. Left: huge Space Grotesk headline
("FORGE YOUR FRAGS INTO REELS" or similar), a one-line subhead, a lime
**"Continue with Steam"** button (Steam glyph), a secondary outline **"Upload a
demo"** button (→ `/upload`, no login) for the file flow, and a trust line *"No
AI. Your POV. Your rig."*. Right: a stylized reel/filmstrip motif (CSS, no real
video). On sign-in → `/connect` if history not linked, else `/matches`.

### `/upload` — Upload a demo (Flow B, no login)
Renders on the root layout (no sidebar) like `/connect`. A centered card with a
dashed **drop zone** (`DemoDropzone`): drag-and-drop or click-to-browse a single
`.dem`, extension-validated. The real flow is **scan → player picker → parse**:
on select it shows a brief "Scanning roster…" state (`scanDemo`), then a
**`PlayerPicker`** ("Who do you want to clip?") listing the demo's players with
mono K/D/A (top fragger auto-highlighted, click to confirm). Picking a player
runs `parseDemo({ jobId, steamId })` ("Forging highlights…") and routes to
`/matches/[id]` — the same find-highlights screen Steam matches use. Uploaded
matches have no round score, so `score` is `''` and `mvps` is `0`; the summary
strip hides the win/loss + score chips and shows the highlight count instead.
The deprecated single-shot `uploadDemo` remains for the mock. Copy leans on
"analyze any demo, yours or anyone's, no login · the .dem never leaves your
machine".

### `/connect` — Onboarding (vertical stepper, not two side cards)
A centered, two-step **stepper**:
1. **Link your match history** — shadcn `Input`s for the auth code and most
   recent sharecode, a helper line + "How to get these" link to Steam, a lime
   "Load my matches" button (calls `linkMatchHistory`, shows matches found,
   advances).
2. **Pair your PC** — explains the BYO-PC agent (your Steam + GPU do the
   recording). "Generate pairing code" (`pairPc`) reveals a copyable mono code
   and a paired/not-paired `Badge` (`getPcStatus`). Then "Enter the studio" →
   `/matches`.
Use a clean numbered stepper rail on the left of the content.

### `/matches` — Matches (the studio's "inbox")
Sidebar + main. Header: Space Grotesk title "Matches" + subhead, a `ToggleGroup`
filter (All / Wins / Best frags) and a search `Input`. Body: a list of
**scoreboard rows** — each row: `ScoreBar` (win/loss), map name + map chip, score
in mono, "N highlights" badge, a row of `StatMono` (K D A MVP K/D), `timeAgo`,
and a lime "Find highlights" button → `/matches/[id]`. Dense but breathable;
hover elevates the row.

### `/matches/[id]` — Find highlights
Top: a compact match summary strip (map, score in mono, key stats). Then
**"We found N highlights"** rendered as a **`Filmstrip`** of play tiles
(thumbnail block, mono round/tick, a kills pip, weapon). Selecting a tile gives
it a lime ring. Below: two large **mode cards** — **Clean POV** and **Music
Edit** — each with an icon, one-line pitch, and (Music) "pick a song next". A
sticky bottom action bar shows the selection and a lime **"Create reel"**.
Choosing Music opens a shadcn **`Dialog`** song picker: each row = title/artist,
a play/preview toggle (no real audio — UI state only, with a tiny equalizer
animation while "playing"), and a lime "Use this track". Confirm → `createVideo`
→ `/videos`.

### `/videos` — Library
Header "Library". Section **Rendering** (status not ready/failed): cards showing
the thumbnail (dimmed), `RecDot` + "LIVE ON YOUR RIG" when capturing, the
**`PipelineSteps`** stepper, and a shimmer. Poll `listVideos()` every ~1.5s so a
fresh job advances Queued→Capturing→Editing→Ready live. Section **Ready**: a grid
of cards — thumbnail with a hover overlay (`View` / `Download` / `Share`), title,
map · score in mono, a subtle "expires in 14h" chip (mono countdown via
`formatCountdown`), and a Publish toggle (`publishVideo`) that flips to a lime
"Published" `Badge`. Use `Skeleton` while loading.

### `/feed` — Feed
"Feed" header + subhead. A responsive **portrait masonry grid** of community
reels: 9:16 thumbnail with a soft gradient foot, author avatar + name, map chip,
and a heart + like count (local toggle, lime when liked). Creator-forward, hover
reveals a play affordance. `Skeleton` while loading.

---

## 6. Voice

Confident, concise, gamer-literate, zero cringe. "Find your highlights." "Your
reel is rendering on your rig." "Ready to post." Avoid hype words and emoji.

---

## 7. What NOT to do

- No violet as the brand color. No centered top-tab navigation. No stat "pills"
  as the primary stat treatment (use mono numbers). No generic stock-SaaS hero.
- Keep `web/lib/api/*` and `useSession()` stable. The contract may grow only
  **additively** when a new flow needs it (e.g. the `uploadDemo` method and the
  optional `Match.source` field added for Flow B, and the `scanDemo`/`parseDemo`
  methods + `DemoPlayer` type added for the real scan→pick→parse upload flow) so
  the Fase 2 real-backend swap stays intact; never change or remove existing
  fields/signatures.
- No real audio, no real video — placeholders only (picsum/dicebear) this phase.
