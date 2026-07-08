import {
  Crosshair,
  Cpu,
  Clapperboard,
  Download,
  Github,
  ShieldCheck,
  MonitorCheck,
  Gamepad2,
  Film,
  Zap,
  HardDrive,
} from "lucide-react";
import type { LucideIcon } from "lucide-react";
import HeroForge from "@/components/hero-forge";

// Canonical download facts, kept in lockstep with the FragForge Studio
// release. The canonical asset name comes from desktop/package.json version.
// The href must remain byte-for-byte this string.
const DOWNLOAD_URL =
  "https://github.com/rechedev9/fragforge/releases/download/v0.3.2/FragForge.Studio.Setup.0.3.2.exe";
const REPO_URL = "https://github.com/rechedev9/fragforge";

// NEON HUD wordmark: FRAG//FORGE in Chakra Petch, the "//" in signal cyan —
// same motif as web/components/brand/wordmark.tsx.
function Wordmark({ size = "md" }: { size?: "md" | "sm" }) {
  const text = size === "sm" ? "text-lg" : "text-2xl";
  return (
    <span className={`font-display font-bold tracking-[0.04em] ${text}`}>
      FRAG
      <span className="text-primary">{"//"}</span>
      FORGE
    </span>
  );
}

// Mono eyebrow with a "//" mark, HUD-style. The mark is aria-hidden so the
// accessible name stays exactly the section label passed in.
function Eyebrow({ children }: { children: React.ReactNode }) {
  return (
    <p className="font-mono text-xs font-medium uppercase tracking-[0.3em] text-primary/90">
      <span aria-hidden>{"// "}</span>
      {children}
    </p>
  );
}

const FEATURES: { icon: LucideIcon; title: string; body: string }[] = [
  {
    icon: Crosshair,
    title: "Demo-accurate",
    body: "Every cut comes from parsing the .dem file, not guesswork. The demo is the source of truth for who, when, and where.",
  },
  {
    icon: Cpu,
    title: "Real capture",
    body: "FragForge drives HLAE + CS2 on your own GPU to record a clean, HUD-less POV of every frag at full quality.",
  },
  {
    icon: Clapperboard,
    title: "Upload-ready",
    body: "Outputs a 9:16 1080x1920 60fps vertical Short with the viral-60-clean edit, ready to post straight away.",
  },
];

const STEPS: { title: string; body: string }[] = [
  {
    title: "Drop your .dem",
    body: "Add a CS2 demo file. FragForge parses it locally in seconds, entirely on your machine.",
  },
  {
    title: "Pick your player & kills",
    body: "Choose whose frags to feature and select the exact kills in a fast, web-style UI.",
  },
  {
    title: "FragForge captures & edits",
    body: "HLAE + CS2 record the gameplay, then it renders hook text, kill punch-ins, a kill counter, slow-mo on the final kill, beat-synced music and auto captions.",
  },
  {
    title: "Post your Short",
    body: "Grab the finished vertical Short and upload it. No account, and your demos never left your PC.",
  },
];

const REQUIREMENTS: { icon: LucideIcon; label: string }[] = [
  { icon: MonitorCheck, label: "Windows 10 or 11, 64-bit" },
  { icon: Gamepad2, label: "Counter-Strike 2 installed via Steam" },
  { icon: Film, label: "HLAE installed (for gameplay capture)" },
  { icon: Zap, label: "Dedicated GPU recommended" },
  { icon: HardDrive, label: "~1 GB free disk space" },
];

export default function Home() {
  return (
    <main className="flex min-h-screen w-full flex-col bg-background">
      {/* 1. HERO -------------------------------------------------------- */}
      <section
        id="hero"
        className="relative flex min-h-screen w-full flex-col items-center justify-center overflow-hidden px-6 py-24 text-center"
      >
        {/* Animated 3D particle forge (or static fallback under reduced motion). */}
        <HeroForge />

        {/* Charcoal scrim so the headline stays readable over the forge glow. */}
        <div
          className="pointer-events-none absolute inset-0 bg-gradient-to-b from-background/70 via-background/20 to-background/85"
          aria-hidden="true"
        />

        <div className="relative z-10 flex max-w-3xl flex-col items-center gap-7">
          <Wordmark />

          <h1 className="font-display text-4xl font-bold uppercase leading-[1.05] tracking-[0.01em] text-foreground [text-shadow:0_0_28px_color-mix(in_oklch,var(--primary)_35%,transparent)] sm:text-6xl">
            Turn CS2 demos into
            <br className="hidden sm:block" /> viral{" "}
            <span className="text-primary">Shorts</span>
          </h1>

          <p className="max-w-xl text-lg text-muted-foreground sm:text-xl">
            A free Windows app that forges your CS2 demo files into upload-ready
            vertical Shorts - captured and edited entirely on your own PC.
          </p>

          <div className="mt-2 flex flex-col items-center gap-4 sm:flex-row">
            <a
              href={DOWNLOAD_URL}
              className="neon-notch neon-glow group inline-flex items-center gap-2.5 bg-primary px-7 py-3.5 font-display text-base font-semibold text-primary-foreground transition hover:brightness-110 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary focus-visible:ring-offset-2 focus-visible:ring-offset-background"
            >
              <Download className="h-5 w-5" strokeWidth={2.5} />
              Download for Windows
            </a>
            <a
              href={REPO_URL}
              className="neon-notch inline-flex items-center gap-2 border-[1.5px] border-primary/40 px-6 py-3.5 text-base font-medium text-foreground/90 transition hover:border-primary hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary focus-visible:ring-offset-2 focus-visible:ring-offset-background"
            >
              <Github className="h-5 w-5" />
              View on GitHub
            </a>
          </div>

          <p className="font-mono text-sm tracking-[0.08em] text-muted-foreground">
            v0.3.2 <span className="text-primary/70">·</span> 152 MB{" "}
            <span className="text-primary/70">·</span> Windows 10/11
          </p>
        </div>
      </section>

      {/* 2. WHAT IT DOES ----------------------------------------------- */}
      <section
        id="what-it-does"
        className="w-full border-t border-border px-6 py-24 sm:py-32"
      >
        <div className="mx-auto max-w-6xl">
          <div className="flex flex-col items-center gap-4 text-center">
            <Eyebrow>Features</Eyebrow>
            <h2 className="font-display text-3xl font-bold tracking-tight text-foreground sm:text-4xl">
              What it does
            </h2>
            <p className="max-w-2xl text-lg text-muted-foreground">
              Not a screen recorder and not a montage tool. FragForge reconstructs
              your frags from the demo and renders them for you.
            </p>
          </div>

          <div className="mt-14 grid gap-6 md:grid-cols-3">
            {FEATURES.map(({ icon: Icon, title, body }) => (
              <div
                key={title}
                className="neon-brackets relative flex flex-col gap-4 border border-border bg-card p-7 transition hover:border-primary/40"
              >
                <span className="flex h-11 w-11 items-center justify-center border border-border bg-background text-primary">
                  <Icon className="h-6 w-6" strokeWidth={2} />
                </span>
                <h3 className="font-display text-xl font-semibold uppercase tracking-[0.02em] text-foreground">
                  {title}
                </h3>
                <p className="text-[15px] leading-relaxed text-muted-foreground">
                  {body}
                </p>
              </div>
            ))}
          </div>
        </div>
      </section>

      {/* 3. HOW IT WORKS ----------------------------------------------- */}
      <section
        id="how-it-works"
        className="w-full border-t border-border px-6 py-24 sm:py-32"
      >
        <div className="mx-auto max-w-6xl">
          <div className="flex flex-col items-center gap-4 text-center">
            <Eyebrow>Workflow</Eyebrow>
            <h2 className="font-display text-3xl font-bold tracking-tight text-foreground sm:text-4xl">
              How it works
            </h2>
            <p className="max-w-2xl text-lg text-muted-foreground">
              From a raw .dem file to a finished vertical Short in four steps.
            </p>
          </div>

          <ol className="mt-14 grid gap-6 md:grid-cols-2 lg:grid-cols-4">
            {STEPS.map(({ title, body }, i) => (
              <li
                key={title}
                className="neon-brackets relative flex flex-col gap-4 border border-border bg-card p-7"
              >
                <span className="font-mono text-3xl font-bold text-primary [text-shadow:0_0_10px_color-mix(in_oklch,var(--primary)_60%,transparent)]">
                  {String(i + 1).padStart(2, "0")}
                </span>
                <h3 className="font-display text-lg font-semibold uppercase tracking-[0.02em] text-foreground">
                  {title}
                </h3>
                <p className="text-[15px] leading-relaxed text-muted-foreground">
                  {body}
                </p>
              </li>
            ))}
          </ol>
        </div>
      </section>

      {/* 4. REQUIREMENTS ----------------------------------------------- */}
      <section
        id="requirements"
        className="w-full border-t border-border px-6 py-24 sm:py-32"
      >
        <div className="mx-auto grid max-w-6xl gap-12 lg:grid-cols-[minmax(0,1fr)_minmax(0,1.4fr)] lg:items-center">
          <div className="flex flex-col items-start gap-4">
            <Eyebrow>Setup</Eyebrow>
            <h2 className="font-display text-3xl font-bold tracking-tight text-foreground sm:text-4xl">
              Requirements
            </h2>
            <p className="max-w-md text-lg text-muted-foreground">
              Parsing a demo is light. Capturing gameplay drives CS2 through HLAE
              on your GPU, so a gaming PC gets the best results.
            </p>
          </div>

          <ul className="neon-brackets relative grid gap-3 border border-border bg-card p-6 sm:p-8">
            {REQUIREMENTS.map(({ icon: Icon, label }) => (
              <li
                key={label}
                className="flex items-center gap-3.5 border-b border-border/60 py-3 last:border-b-0"
              >
                <span className="flex h-9 w-9 shrink-0 items-center justify-center border border-border bg-background text-primary">
                  <Icon className="h-5 w-5" strokeWidth={2} />
                </span>
                <span className="text-[15px] text-foreground">{label}</span>
              </li>
            ))}
          </ul>
        </div>
      </section>

      {/* 5. SMARTSCREEN NOTE ------------------------------------------- */}
      <section
        id="smartscreen"
        className="w-full border-t border-border px-6 py-24 sm:py-28"
      >
        <div className="neon-brackets relative mx-auto flex max-w-3xl flex-col gap-5 border border-border bg-card p-7 sm:flex-row sm:items-start sm:gap-6 sm:p-8">
          <span className="flex h-11 w-11 shrink-0 items-center justify-center border border-border bg-background text-primary">
            <ShieldCheck className="h-6 w-6" strokeWidth={2} />
          </span>
          <div className="flex flex-col gap-2.5">
            <h2 className="font-display text-xl font-semibold text-foreground">
              A note on the unsigned installer
            </h2>
            <p className="text-[15px] leading-relaxed text-muted-foreground">
              FragForge Studio is a free, local tool, so the installer is not
              code-signed yet. On first run Windows SmartScreen may show{" "}
              <span className="font-medium text-foreground">
                &ldquo;Windows protected your PC&rdquo;
              </span>
              . That is expected: click{" "}
              <span className="font-mono text-foreground">More info</span>, then{" "}
              <span className="font-mono text-foreground">Run anyway</span> to
              continue. The source is open on GitHub if you want to inspect it
              first.
            </p>
          </div>
        </div>
      </section>

      {/* 6. FOOTER ------------------------------------------------------ */}
      <footer className="w-full border-t border-border px-6 py-12">
        <div className="mx-auto flex max-w-6xl flex-col items-center justify-between gap-6 text-center sm:flex-row sm:text-left">
          <div className="flex flex-col items-center gap-2 sm:items-start">
            <Wordmark size="sm" />
            <p className="text-sm text-muted-foreground">
              Free &amp; local - your demos never leave your PC.
            </p>
          </div>

          <div className="flex flex-col items-center gap-2 sm:items-end">
            <a
              href={REPO_URL}
              className="inline-flex items-center gap-2 text-sm font-medium text-foreground/90 transition hover:text-primary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary focus-visible:ring-offset-2 focus-visible:ring-offset-background"
            >
              <Github className="h-4 w-4" />
              GitHub repository
            </a>
            <p className="font-mono text-xs text-muted-foreground">
              © 2026 FragForge
            </p>
          </div>
        </div>
      </footer>
    </main>
  );
}
