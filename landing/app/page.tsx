import {
  ArrowRight,
  CheckCircle2,
  Clapperboard,
  Cpu,
  Crosshair,
  Download,
  Film,
  Gamepad2,
  Gauge,
  Github,
  HardDrive,
  LockKeyhole,
  MonitorCheck,
  Play,
  ShieldCheck,
  Sparkles,
  Target,
  Zap,
} from "lucide-react";
import type { LucideIcon } from "lucide-react";
import HeroForge from "@/components/hero-forge";
import Reveal from "@/components/reveal";

const DOWNLOAD_URL =
  "https://github.com/rechedev9/fragforge/releases/download/v2.2.13/FragForge.Studio.Setup.2.2.13.exe";
const RELEASE_VERSION = "v2.2.13";
const REPO_URL = "https://github.com/rechedev9/fragforge";

function Corners() {
  return (
    <>
      <span aria-hidden="true" className="pointer-events-none absolute -left-px -top-px h-4 w-4 border-l-2 border-t-2 border-cyan-300" />
      <span aria-hidden="true" className="pointer-events-none absolute -bottom-px -right-px h-4 w-4 border-b-2 border-r-2 border-cyan-300" />
    </>
  );
}

function Wordmark({ compact = false }: { compact?: boolean }) {
  return (
    <span className="inline-flex items-center gap-3">
      <span className="relative grid size-9 place-items-center border border-cyan-300/50 bg-cyan-300/10 text-cyan-300 shadow-[0_0_24px_rgba(34,217,238,0.18)]">
        <Crosshair className="size-5" strokeWidth={1.8} />
        <span className="absolute -right-1 -top-1 size-2 bg-pink-500" />
      </span>
      <span className={compact ? "text-lg font-bold tracking-[0.08em]" : "text-xl font-bold tracking-[0.08em]"}>
        FRAG<span className="text-cyan-300">//</span>FORGE
      </span>
    </span>
  );
}

function Eyebrow({ children }: { children: React.ReactNode }) {
  return (
    <p className="inline-flex items-center gap-3 font-mono text-xs font-semibold uppercase tracking-[0.28em] text-cyan-300">
      <span className="h-px w-8 bg-cyan-300/70" aria-hidden="true" />
      {children}
    </p>
  );
}

const FEATURES: {
  icon: LucideIcon;
  title: string;
  body: string;
  signal: string;
  className: string;
}[] = [
  {
    icon: Target,
    title: "The demo is the source of truth",
    body: "FragForge parses every tick, identifies the exact killer, victim and round, then builds the recording plan from real match data.",
    signal: "TICK-PERFECT",
    className: "md:col-span-7",
  },
  {
    icon: Cpu,
    title: "Your GPU. Real CS2.",
    body: "The official HLAE release drives CS2 locally and captures clean, high-fidelity POV footage instead of faking the moment.",
    signal: "120 FPS CAPTURE",
    className: "md:col-span-5",
  },
  {
    icon: Clapperboard,
    title: "The edit lands ready to post",
    body: "Native killfeed, hook text, punch-ins, kill counter, final-kill slow motion and a pristine 1080x1920 render — all assembled automatically.",
    signal: "1080 × 1920 · 60 FPS",
    className: "md:col-span-7",
  },
  {
    icon: Sparkles,
    title: "Stream clips speak clearly",
    body: "Cut Twitch or YouTube moments and optionally turn their speech into word-timed, burned-in subtitles through xAI — ready for vertical viewing.",
    signal: "XAI WORD TIMESTAMPS",
    className: "md:col-span-5",
  },
];

const STEPS = [
  { title: "Add the source", body: "Drop a .dem or paste a stream URL. Demo parsing and capture stay on your PC." },
  { title: "Choose the story", body: "Pick the player, round and exact kills worth turning into a Short." },
  { title: "Let the forge run", body: "FragForge records the real POV or cuts the stream, then composes the vertical edit and optional xAI captions." },
  { title: "Post the result", body: "Prepare the metadata, download the MP4 and open YouTube Studio to finish the official upload flow." },
];

const REQUIREMENTS: { icon: LucideIcon; label: string; detail: string }[] = [
  { icon: MonitorCheck, label: "Windows 10 / 11", detail: "64-bit desktop" },
  { icon: Gamepad2, label: "Counter-Strike 2", detail: "Installed through Steam" },
  { icon: Film, label: "Official HLAE 2.191.1", detail: "Bundled and installed automatically" },
  { icon: Zap, label: "Dedicated GPU", detail: "Recommended for capture" },
  { icon: HardDrive, label: "~1 GB", detail: "Free disk space" },
];

export default function Home() {
  return (
    <main className="min-h-screen overflow-hidden bg-[#050812] text-[#f2fbff] [font-family:var(--font-chakra-petch)]">
      <section id="hero" className="relative isolate flex min-h-[900px] w-full flex-col overflow-hidden border-b border-cyan-300/15 lg:min-h-screen">
        <HeroForge />

        <nav aria-label="Primary navigation" className="relative z-30 mx-auto flex w-full max-w-[1480px] items-center justify-between px-5 py-5 sm:px-8 lg:px-12">
          <a href="#hero" className="rounded-sm outline-none transition-opacity hover:opacity-80 focus-visible:ring-2 focus-visible:ring-cyan-300">
            <Wordmark />
          </a>
          <div className="hidden items-center gap-8 text-sm font-medium text-slate-300 lg:flex">
            <a className="transition hover:text-cyan-300" href="#what-it-does">Engine</a>
            <a className="transition hover:text-cyan-300" href="#how-it-works">Workflow</a>
            <a className="transition hover:text-cyan-300" href="#requirements">Requirements</a>
          </div>
          <a href={DOWNLOAD_URL} className="group inline-flex items-center gap-2 border border-cyan-300/40 bg-slate-950/55 px-4 py-2.5 text-sm font-semibold text-white shadow-[0_0_28px_rgba(34,217,238,0.1)] backdrop-blur-md transition duration-300 hover:border-cyan-300 hover:bg-cyan-300 hover:text-slate-950 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-cyan-300">
            <span className="sm:hidden">Get app</span>
            <span className="hidden sm:inline">Get FragForge</span>
            <ArrowRight className="size-4 transition-transform duration-300 group-hover:translate-x-1" />
          </a>
        </nav>

        <div className="relative z-20 mx-auto grid w-full max-w-[1480px] flex-1 items-center gap-10 px-5 pb-24 pt-12 sm:px-8 lg:grid-cols-[minmax(0,0.9fr)_minmax(420px,1.1fr)] lg:px-12 lg:pb-20 lg:pt-8">
          <div className="max-w-3xl">
            <div className="mb-7 inline-flex items-center gap-3 border border-cyan-300/20 bg-slate-950/60 px-3 py-2 font-mono text-[11px] uppercase tracking-[0.22em] text-slate-300 backdrop-blur-md">
              <span className="relative flex size-2">
                <span className="absolute inline-flex size-full animate-ping bg-emerald-400 opacity-70 motion-reduce:animate-none" />
                <span className="relative inline-flex size-2 bg-emerald-400" />
              </span>
              Capture pipeline online
              <span className="text-cyan-300">{RELEASE_VERSION}</span>
            </div>

            <h1 className="max-w-[900px] text-balance text-5xl font-bold uppercase leading-[0.94] tracking-[-0.035em] text-white drop-shadow-2xl sm:text-7xl lg:text-[clamp(4.5rem,7vw,7rem)]">
              Your best frags.
              <span className="mt-2 block bg-gradient-to-r from-cyan-200 via-cyan-300 to-pink-500 bg-clip-text text-transparent">
                Forged to go viral.
              </span>
            </h1>

            <p className="mt-7 max-w-2xl text-pretty text-lg leading-relaxed text-slate-300 sm:text-xl">
              Turn raw CS2 demos and stream moments into upload-ready vertical Shorts with real HLAE capture, native killfeed and optional xAI subtitles. Capture and rendering stay on your PC.
            </p>

            <div className="mt-9 flex flex-col gap-3 sm:flex-row sm:items-center">
              <a href={DOWNLOAD_URL} className="group inline-flex min-h-14 items-center justify-center gap-3 bg-cyan-300 px-7 text-base font-bold text-slate-950 shadow-[0_0_44px_rgba(34,217,238,0.32)] transition duration-300 hover:-translate-y-1 hover:bg-white hover:shadow-[0_0_64px_rgba(34,217,238,0.5)] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-white focus-visible:ring-offset-2 focus-visible:ring-offset-slate-950">
                <Download className="size-5" strokeWidth={2.4} />
                Download for Windows
                <ArrowRight className="size-4 transition-transform duration-300 group-hover:translate-x-1" />
              </a>
              <a href={REPO_URL} className="group inline-flex min-h-14 items-center justify-center gap-3 border border-white/15 bg-white/5 px-7 text-base font-semibold text-white backdrop-blur-md transition duration-300 hover:-translate-y-1 hover:border-white/35 hover:bg-white/10 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-cyan-300">
                <Github className="size-5" />
                View on GitHub
              </a>
            </div>

            <div className="mt-7 flex flex-wrap gap-x-6 gap-y-3 font-mono text-xs uppercase tracking-[0.12em] text-slate-400">
              <span className="inline-flex items-center gap-2"><LockKeyhole className="size-4 text-cyan-300" />Local capture</span>
              <span className="inline-flex items-center gap-2"><Gauge className="size-4 text-cyan-300" />60 FPS output</span>
              <span className="inline-flex items-center gap-2"><CheckCircle2 className="size-4 text-cyan-300" />No FragForge account</span>
            </div>
          </div>

          <div className="pointer-events-none relative hidden min-h-[600px] lg:block" aria-hidden="true">
            <div className="absolute right-[7%] top-[10%] w-64 animate-pulse border border-cyan-300/30 bg-slate-950/65 p-4 shadow-[0_0_50px_rgba(34,217,238,0.16)] backdrop-blur-xl motion-reduce:animate-none">
              <Corners />
              <div className="flex items-center justify-between font-mono text-[10px] uppercase tracking-[0.22em] text-slate-400">
                <span>Live analysis</span><span className="text-emerald-400">Ready</span>
              </div>
              <div className="mt-4 flex items-end justify-between">
                <div>
                  <p className="text-3xl font-bold text-white">5K</p>
                  <p className="mt-1 font-mono text-xs text-cyan-300">ROUND 09 · INFERNO</p>
                </div>
                <Target className="size-9 text-cyan-300" strokeWidth={1.5} />
              </div>
            </div>

            <div className="absolute bottom-[12%] right-[2%] w-72 border border-white/15 bg-slate-950/70 p-4 backdrop-blur-xl">
              <div className="flex items-center gap-3">
                <span className="grid size-10 place-items-center bg-pink-500/15 text-pink-400"><Play className="size-5 fill-current" /></span>
                <div>
                  <p className="font-mono text-[10px] uppercase tracking-[0.2em] text-slate-400">Output forged</p>
                  <p className="mt-1 font-semibold text-white">1080 × 1920 · 60 FPS</p>
                </div>
              </div>
              <div className="mt-4 h-1 overflow-hidden bg-white/10"><div className="h-full w-full bg-gradient-to-r from-cyan-300 to-pink-500" /></div>
            </div>
          </div>
        </div>

        <a href="#what-it-does" aria-label="Explore the FragForge engine" className="absolute bottom-7 left-1/2 z-30 hidden -translate-x-1/2 flex-col items-center gap-2 font-mono text-[10px] uppercase tracking-[0.24em] text-slate-400 transition hover:text-cyan-300 lg:flex">
          Explore the engine
          <span className="h-8 w-px animate-pulse bg-gradient-to-b from-cyan-300 to-transparent motion-reduce:animate-none" />
        </a>
      </section>

      <section aria-label="FragForge product facts" className="relative border-b border-cyan-300/15 bg-[#070c18]">
        <div className="mx-auto grid max-w-[1480px] divide-y divide-cyan-300/10 px-5 sm:px-8 md:grid-cols-3 md:divide-x md:divide-y-0 lg:px-12">
          {[
            ["ZERO CLOUD", "Your demo never leaves the machine"],
            ["REAL CAPTURE", "HLAE + CS2 on your own GPU"],
            ["POST READY", "MP4, cover and caption in one pack"],
          ].map(([title, body]) => (
            <div key={title} className="flex items-center gap-4 py-5 md:px-7">
              <span className="size-2 shrink-0 bg-cyan-300 shadow-[0_0_18px_rgba(34,217,238,0.75)]" />
              <div>
                <p className="font-mono text-xs font-semibold tracking-[0.2em] text-cyan-300">{title}</p>
                <p className="mt-1 text-sm text-slate-400">{body}</p>
              </div>
            </div>
          ))}
        </div>
      </section>

      <section id="what-it-does" className="relative border-b border-white/10 bg-[#050812] px-5 py-24 sm:px-8 sm:py-32 lg:px-12">
        <div aria-hidden="true" className="absolute inset-0 bg-[linear-gradient(rgba(34,217,238,0.035)_1px,transparent_1px),linear-gradient(90deg,rgba(34,217,238,0.035)_1px,transparent_1px)] bg-[size:48px_48px] [mask-image:linear-gradient(to_bottom,transparent,black_18%,black_82%,transparent)]" />
        <div className="relative mx-auto max-w-[1320px]">
          <Reveal>
            <div className="max-w-3xl">
              <Eyebrow>Inside the forge</Eyebrow>
              <h2 className="mt-5 text-balance text-4xl font-bold uppercase tracking-[-0.025em] text-white sm:text-6xl">What it does</h2>
              <p className="mt-6 max-w-2xl text-lg leading-relaxed text-slate-400">
                Not another screen recorder. FragForge reconstructs the moment from the demo, captures it in the game and finishes the edit.
              </p>
            </div>
          </Reveal>

          <div className="mt-14 grid gap-5 md:grid-cols-12">
            {FEATURES.map(({ icon: Icon, title, body, signal, className }, index) => (
              <Reveal key={title} className={className} delay={index}>
                <article className="group relative h-full min-h-72 overflow-hidden border border-white/10 bg-slate-900/45 p-7 backdrop-blur-sm transition duration-500 hover:-translate-y-1 hover:border-cyan-300/40 hover:bg-slate-900/70 sm:p-9">
                  <Corners />
                  <div className="flex h-full flex-col">
                    <div className="flex items-start justify-between gap-6">
                      <span className="grid size-12 place-items-center border border-cyan-300/25 bg-cyan-300/10 text-cyan-300 transition duration-500 group-hover:border-cyan-300/60 group-hover:shadow-[0_0_35px_rgba(34,217,238,0.2)]"><Icon className="size-6" strokeWidth={1.8} /></span>
                      <span className="font-mono text-[10px] uppercase tracking-[0.2em] text-cyan-300/80">{signal}</span>
                    </div>
                    <h3 className="mt-8 max-w-2xl text-2xl font-bold uppercase tracking-[-0.015em] text-white sm:text-3xl">{title}</h3>
                    <p className="mt-4 max-w-2xl text-base leading-relaxed text-slate-400">{body}</p>
                    <div className="mt-auto pt-8"><div className="h-px w-full overflow-hidden bg-white/10"><div className="h-full w-1/3 bg-gradient-to-r from-cyan-300 to-transparent transition-all duration-700 group-hover:w-full" /></div></div>
                  </div>
                </article>
              </Reveal>
            ))}
          </div>
        </div>
      </section>

      <section id="how-it-works" className="relative border-b border-white/10 bg-[#080d19] px-5 py-24 sm:px-8 sm:py-32 lg:px-12">
        <div className="mx-auto max-w-[1320px]">
          <Reveal>
            <div className="flex flex-col justify-between gap-8 lg:flex-row lg:items-end">
              <div>
                <Eyebrow>Four beats. One Short.</Eyebrow>
                <h2 className="mt-5 text-4xl font-bold uppercase tracking-[-0.025em] text-white sm:text-6xl">How it works</h2>
              </div>
              <p className="max-w-xl text-lg leading-relaxed text-slate-400">
                From a raw match file to a vertical highlight without timelines, capture hotkeys or manual reframing.
              </p>
            </div>
          </Reveal>

          <ol className="mt-16 grid gap-px overflow-hidden border border-white/10 bg-white/10 lg:grid-cols-4">
            {STEPS.map(({ title, body }, index) => (
              <li key={title} className="group relative min-h-80 bg-[#080d19] p-7 transition duration-500 hover:bg-slate-900/80 sm:p-8">
                <div className="flex items-center justify-between">
                  <span className="font-mono text-5xl font-bold text-cyan-300/25 transition duration-500 group-hover:text-cyan-300">{String(index + 1).padStart(2, "0")}</span>
                  <ArrowRight className="size-5 text-slate-600 transition duration-500 group-hover:translate-x-1 group-hover:text-cyan-300" />
                </div>
                <h3 className="mt-14 text-xl font-bold uppercase text-white">{title}</h3>
                <p className="mt-4 text-[15px] leading-relaxed text-slate-400">{body}</p>
                <span className="absolute bottom-0 left-0 h-1 w-0 bg-gradient-to-r from-cyan-300 to-pink-500 transition-all duration-500 group-hover:w-full" />
              </li>
            ))}
          </ol>
        </div>
      </section>

      <section className="relative border-b border-white/10 bg-[#050812] px-5 py-24 sm:px-8 sm:py-32 lg:px-12">
        <div className="mx-auto grid max-w-[1320px] items-center gap-16 lg:grid-cols-[minmax(0,1fr)_minmax(380px,0.8fr)]">
          <Reveal>
            <div>
              <Eyebrow>The output</Eyebrow>
              <h2 className="mt-5 max-w-3xl text-balance text-4xl font-bold uppercase tracking-[-0.025em] text-white sm:text-6xl">
                The killfeed survives.<span className="block text-cyan-300">The moment hits harder.</span>
              </h2>
              <p className="mt-7 max-w-2xl text-lg leading-relaxed text-slate-400">
                FragForge keeps the native CS2 death notices inside the vertical safe area, filters them to your player and builds the edit around the real action.
              </p>
              <ul className="mt-8 grid gap-4 sm:grid-cols-2">
                {["Native, readable killfeed", "Target-player filtering", "Hook and kill counter", "Upload-ready publish pack"].map((item) => (
                  <li key={item} className="flex items-center gap-3 text-slate-200"><CheckCircle2 className="size-5 shrink-0 text-cyan-300" />{item}</li>
                ))}
              </ul>
            </div>
          </Reveal>

          <Reveal className="relative mx-auto w-full max-w-[430px]" delay={1}>
            <div aria-hidden="true" className="absolute inset-8 animate-pulse bg-cyan-300/20 blur-3xl motion-reduce:animate-none" />
            <div className="relative mx-auto aspect-[9/16] w-[min(78vw,330px)] border border-cyan-300/35 bg-[url('/images/hero-replay-forge.webp')] bg-cover bg-[position:76%_center] p-4 shadow-[0_0_80px_rgba(34,217,238,0.18)]">
              <Corners />
              <div className="absolute inset-0 bg-gradient-to-b from-slate-950/10 via-transparent to-slate-950/85" />
              <div className="relative flex items-center justify-between font-mono text-[10px] uppercase tracking-[0.18em] text-white/70">
                <span>Round 09</span>
                <span className="inline-flex items-center gap-2 text-emerald-300"><span className="size-1.5 animate-pulse bg-emerald-300 motion-reduce:animate-none" />Live</span>
              </div>
              <div className="absolute right-4 top-[20%] grid gap-1.5 text-[10px] font-semibold sm:text-xs">
                {["vini  AK-47  Leomonster", "vini  AK-47  zmb", "vini  AK-47  Divine"].map((kill) => (
                  <div key={kill} className="border-l-2 border-cyan-300 bg-slate-950/85 px-3 py-2 text-white shadow-lg backdrop-blur-sm">{kill}</div>
                ))}
              </div>
              <div className="absolute inset-x-4 bottom-4 border border-white/15 bg-slate-950/80 p-4 backdrop-blur-md">
                <div className="flex items-end justify-between">
                  <div><p className="font-mono text-[10px] uppercase tracking-[0.22em] text-cyan-300">Export ready</p><p className="mt-1 text-2xl font-bold text-white">ACE</p></div>
                  <p className="font-mono text-xs text-slate-300">5 / 5</p>
                </div>
                <div className="mt-3 h-1 bg-white/10"><div className="h-full w-full bg-gradient-to-r from-cyan-300 to-pink-500" /></div>
              </div>
            </div>
          </Reveal>
        </div>
      </section>

      <section id="requirements" className="border-b border-white/10 bg-[#080d19] px-5 py-24 sm:px-8 sm:py-28 lg:px-12">
        <div className="mx-auto max-w-[1320px]">
          <Reveal>
            <div className="grid gap-12 lg:grid-cols-[0.72fr_1.28fr] lg:items-center">
              <div>
                <Eyebrow>Ready when you are</Eyebrow>
                <h2 className="mt-5 text-4xl font-bold uppercase tracking-[-0.025em] text-white sm:text-5xl">Requirements</h2>
                <p className="mt-6 max-w-lg text-lg leading-relaxed text-slate-400">
                  Parsing is light. Real capture runs CS2 on your GPU, so a gaming PC gives the cleanest result.
                </p>
              </div>
              <ul className="relative grid gap-px border border-white/10 bg-white/10 sm:grid-cols-2">
                <Corners />
                {REQUIREMENTS.map(({ icon: Icon, label, detail }, index) => (
                  <li key={label} className={index === REQUIREMENTS.length - 1 ? "flex items-center gap-4 bg-slate-950/80 p-5 sm:col-span-2" : "flex items-center gap-4 bg-slate-950/80 p-5"}>
                    <span className="grid size-11 shrink-0 place-items-center bg-cyan-300/10 text-cyan-300"><Icon className="size-5" strokeWidth={1.8} /></span>
                    <div><p className="font-semibold text-white">{label}</p><p className="mt-1 text-sm text-slate-500">{detail}</p></div>
                  </li>
                ))}
              </ul>
            </div>
          </Reveal>
        </div>
      </section>

      <section id="smartscreen" className="bg-[#050812] px-5 py-16 sm:px-8 lg:px-12">
        <Reveal>
          <div className="relative mx-auto flex max-w-[1040px] flex-col gap-6 border border-amber-300/20 bg-amber-300/[0.035] p-6 sm:flex-row sm:items-start sm:p-8">
            <span className="grid size-12 shrink-0 place-items-center border border-amber-300/25 bg-amber-300/10 text-amber-200"><ShieldCheck className="size-6" /></span>
            <div>
              <h2 className="text-xl font-bold text-white">A transparent note about SmartScreen</h2>
              <p className="mt-3 leading-relaxed text-slate-400">
                FragForge Studio is free and the installer is not code-signed yet. Windows may show{" "}
                <span className="font-semibold text-white">&ldquo;Windows protected your PC&rdquo;</span>. Choose{" "}
                <span className="font-mono text-white">More info</span> and <span className="font-mono text-white">Run anyway</span>.
                The full source and release checksum are public on GitHub.
              </p>
            </div>
          </div>
        </Reveal>
      </section>

      <section className="relative isolate overflow-hidden border-y border-cyan-300/20 px-5 py-24 text-center sm:px-8 sm:py-32 lg:px-12">
        <div aria-hidden="true" className="absolute inset-0 -z-20 bg-[url('/images/hero-replay-forge.webp')] bg-cover bg-[position:62%_center]" />
        <div aria-hidden="true" className="absolute inset-0 -z-10 bg-[linear-gradient(90deg,rgba(5,8,18,0.97),rgba(5,8,18,0.78),rgba(5,8,18,0.9))]" />
        <Reveal>
          <div className="mx-auto flex max-w-4xl flex-col items-center">
            <Sparkles className="size-9 text-cyan-300" />
            <h2 className="mt-6 text-balance text-4xl font-bold uppercase tracking-[-0.03em] text-white sm:text-6xl">
              Your demo already has the story.<span className="block text-cyan-300">Forge the Short.</span>
            </h2>
            <p className="mt-6 max-w-2xl text-lg text-slate-300">
              Free core, local capture and built for players who want the real moment — not another generic montage.
            </p>
            <a href={DOWNLOAD_URL} className="group mt-9 inline-flex min-h-14 items-center gap-3 bg-cyan-300 px-8 font-bold text-slate-950 shadow-[0_0_50px_rgba(34,217,238,0.35)] transition duration-300 hover:-translate-y-1 hover:bg-white focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-white">
              <Download className="size-5" />Download FragForge Studio
              <ArrowRight className="size-4 transition-transform duration-300 group-hover:translate-x-1" />
            </a>
            <p className="mt-4 font-mono text-xs uppercase tracking-[0.18em] text-slate-400">{RELEASE_VERSION} · 150 MB · Windows 10/11</p>
          </div>
        </Reveal>
      </section>

      <footer className="bg-[#040711] px-5 py-10 sm:px-8 lg:px-12">
        <div className="mx-auto flex max-w-[1320px] flex-col items-center justify-between gap-7 text-center sm:flex-row sm:text-left">
          <div><Wordmark compact /><p className="mt-3 text-sm text-slate-500">Free core and local capture. Optional stream-caption audio goes only to xAI when enabled.</p></div>
          <div className="flex flex-col items-center gap-3 sm:items-end">
            <a href={REPO_URL} className="inline-flex items-center gap-2 text-sm font-semibold text-slate-300 transition hover:text-cyan-300 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-cyan-300"><Github className="size-4" />GitHub repository</a>
            <p className="font-mono text-xs text-slate-600">© 2026 FragForge</p>
          </div>
        </div>
      </footer>
    </main>
  );
}
