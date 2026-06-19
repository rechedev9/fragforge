# FragForge — building with this design system

FragForge is **"the replay studio"**: it turns a player's own Counter-Strike 2
demos into highlight reels, recorded on their own rig. Design it like a focused
creator/editing tool — Linear / Vercel / a broadcast room — **not** a marketing
SaaS dashboard. The bar is a premium product that bills a million dollars:
confident, fast, simple, and low-noise, with a real gaming edge. Lean into the
one thing that is ours — a transparent **capture → edit → reel** pipeline that
runs on the player's PC. Earn the gaming feel through restraint and the
signature components below, never through clutter.

## Setup (read first)

- **Forced dark.** Render under a dark root — put `className="dark"` on a top
  wrapper (the app sets it on `<html>`). Every color token then resolves to the
  dark theme and components' `dark:` variants apply. Without it, `text-foreground`
  is near-white on white and disappears.
- **Fonts are loaded.** Headings and the wordmark use Space Grotesk via
  `font-[family-name:var(--font-display)]` (uppercase + tight tracking for
  section eyebrows). Body/UI is Inter (the default). **Every number — score,
  K/D, ADR, tick, duration, pairing code — uses
  `font-[family-name:var(--font-mono)]` (JetBrains Mono) with `tabular-nums`.**
  Mono tabular figures are the scoreboard/demo-tick feel; use them everywhere a
  stat appears.

## Styling idiom: Tailwind v4 + semantic tokens

Style with Tailwind utility classes bound to the shadcn token palette — never
hard-coded hex colors. Vocabulary:

- Surfaces: `bg-background` (deep cool charcoal page), `bg-card`, `bg-popover`,
  `bg-secondary`, `bg-muted`, `bg-sidebar`. Text: `text-foreground`,
  `text-muted-foreground`.
- **Acid-lime is the one signal color** — `bg-primary` / `text-primary` (the
  focus ring is lime too, built into the interactive components). Use it
  *sparingly*: the primary CTA, the active nav item, the
  brand mark, focus rings, win bars, and the done/active pipeline step. Overusing
  lime reads cheap; restraint reads premium. Everything else stays neutral
  charcoal + zinc.
- `bg-destructive` (red) is reserved for the live REC dot and destructive
  actions only. **Win = lime, loss = muted zinc (never red).**
- Hairline 1px borders (`border-border`), never heavy. Radius: cards
  `rounded-xl`, controls `rounded-md`, pills/badges `rounded-full`. Generous
  whitespace, content max ~1200px, fast (150–200ms) ease-out motion; honor
  `prefers-reduced-motion`.

## Components (compose these, not raw HTML)

Primitives: `Button` (variants default[lime] / secondary / outline / ghost /
destructive / link; sizes sm / default / lg / icon), `Badge`, `Card` (with
`CardHeader` / `CardTitle` / `CardDescription` / `CardContent` / `CardFooter` /
`CardAction`), `Input`, `Label`, `Dialog`, `Sheet`, `DropdownMenu`, `Tabs`,
`Tooltip`, `Toggle` / `ToggleGroup`, `Progress`, `ScrollArea`, `Separator`,
`Skeleton`, `Avatar`, `Sidebar`, `Toaster`.

**Signature brand pieces — these are the product's identity, reach for them:**
`StatMono` (every labeled number), `PipelineSteps` (Queued → Capturing →
Editing → Ready — the hero of the product story), `RecDot` ("LIVE ON YOUR RIG"),
`ScoreBar` (win/loss accent bar), `Filmstrip` (horizontal play selector),
`Wordmark`, `SectionEyebrow`, `ReelCover`, `GrainOverlay` (subtle tape texture).
Feature compositions to imitate: `MatchRow`, `ReadyCard`, `FeedCard`,
`PlayerPicker`.

Read each component's `<Name>.prompt.md` for its props and a real usage example,
and `styles.css` (plus its `@import` closure) for the exact token values.

## Idiomatic snippet

```tsx
<div className="dark">
  <Card className="p-4">
    <div className="flex items-center gap-6">
      <ScoreBar win />
      <span className="font-[family-name:var(--font-display)] font-semibold tracking-tight">
        Mirage
      </span>
      <StatMono label="K/D" value="2.21" accent />
      <Button className="ml-auto">Find highlights</Button>
    </div>
  </Card>
</div>
```

Voice: confident, concise, gamer-literate, zero cringe — "Find your highlights.",
"Rendering on your rig.", "Ready to post." No hype words, no emoji.
