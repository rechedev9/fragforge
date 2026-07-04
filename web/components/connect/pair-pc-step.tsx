'use client';

import { useRouter } from 'next/navigation';
import { Button } from '@/components/ui/button';
import { SectionEyebrow } from '@/components/brand/section-eyebrow';
import { AgentConnection } from '@/components/agent/agent-connection';
import { isHostedMode } from '@/lib/mode';

// The FragForge Studio Windows installer, the same canonical release asset
// the landing page's download CTA points at. Kept as a literal here since
// web/ and landing/ are separate Next apps with no shared config; bump both
// in lockstep on a new release.
const AGENT_DOWNLOAD_URL =
  'https://github.com/rechedev9/fragforge/releases/download/v0.2.13/FragForge.Studio.Setup.0.2.13.exe';

export type PairPcStepProps = {
  /**
   * Called when the player chooses to enter the studio. Falls back to a
   * /matches navigation when omitted (e.g. standalone previews).
   */
  onEnter?: () => void;
};

/**
 * Step 2 - connect the player's own PC. Hosted mode renders the AgentConnection
 * panel, where the browser bridges to the LOCAL agent by URL + pasted token;
 * local/cloud render install-and-enter guidance for the agent that records on
 * the user's rig. A thin selector keeps the two variants' hooks unconditional
 * (rules of hooks).
 */
export function PairPcStep(props: PairPcStepProps = {}) {
  if (isHostedMode()) return <HostedPairStep onEnter={props.onEnter} />;
  return <InstallAgentStep onEnter={props.onEnter} />;
}

/**
 * Hosted-mode step 2: the browser bridges directly to the local agent, so there
 * is no cloud pairing code - the AgentConnection panel handles URL + token +
 * connected/disconnected + download instructions.
 */
function HostedPairStep({ onEnter }: { onEnter?: () => void }) {
  const router = useRouter();
  function enter() {
    if (onEnter) onEnter();
    else router.push('/matches');
  }
  return (
    <div className="text-left">
      <SectionEyebrow number={2} label="CONECTA TU AGENTE" />
      <h1 className="mt-2 font-[family-name:var(--font-display)] text-2xl font-bold uppercase tracking-tight text-foreground">
        Conecta tu agente
      </h1>
      <p className="mt-2 text-sm leading-relaxed text-muted-foreground">
        FragForge procesa y graba en tu propio equipo. Ejecuta el agente en este PC y conéctalo con su
        token: nada pesado ni ningún vídeo pasa por nuestros servidores.
      </p>

      <AgentConnection className="mt-6" />

      <Button
        variant="outline"
        size="lg"
        className="mt-6 w-full font-[family-name:var(--font-display)] font-semibold tracking-[0.06em]"
        onClick={enter}
      >
        Entrar al estudio
      </Button>
    </div>
  );
}

/**
 * Local/cloud step 2 - install the agent that records on the player's own rig.
 * FragForge captures with their Steam and GPU, so this points them at the agent
 * download and the capture env vars, then into the studio. Capture readiness is
 * confirmed later by the sidebar CAPTURA card, so this step stays informational.
 */
function InstallAgentStep({ onEnter }: { onEnter?: () => void }) {
  const router = useRouter();

  function enter() {
    if (onEnter) onEnter();
    else router.push('/matches');
  }

  return (
    <div className="text-left">
      <SectionEyebrow number={2} label="INSTALA TU AGENTE" />
      <h1 className="mt-2 font-[family-name:var(--font-display)] text-2xl font-bold uppercase tracking-tight text-foreground">
        Instala tu agente
      </h1>
      <p className="mt-2 text-sm leading-relaxed text-muted-foreground">
        FragForge graba en tu propio equipo. Ejecuta el agente en tu PC gaming y
        captura tus jugadas con tu Steam y tu GPU — tu POV, tu hardware.
      </p>

      <ol className="mt-5 space-y-2.5 text-sm text-muted-foreground">
        <Step n={1}>Instala el agente FragForge en tu PC gaming.</Step>
        <Step n={2}>Apunta el orquestador a HLAE + CS2 con las variables de abajo.</Step>
        <Step n={3}>Entra al estudio y sube tu primera demo.</Step>
      </ol>

      <div className="mt-5 border border-border bg-card/50 p-4">
        <p className="text-sm font-medium text-foreground">La captura necesita HLAE + CS2 en este PC</p>
        <p className="mt-1 text-sm leading-relaxed text-muted-foreground">
          La grabación controla CS2 a través de HLAE. Apunta el orquestador a ellos con
          estas variables de entorno y reinícialo:
        </p>
        <ul className="mt-2 space-y-1 font-[family-name:var(--font-mono)] text-xs text-muted-foreground">
          <li>ZV_RECORDER_PATH</li>
          <li>ZV_HLAE_PATH</li>
          <li>ZV_CS2_PATH</li>
        </ul>
        <p className="mt-2 text-xs text-muted-foreground/80">
          La tarjeta CAPTURA del panel lateral muestra si están configuradas y accesibles.
        </p>
      </div>

      <div className="mt-6 flex flex-col gap-3">
        <a
          href={AGENT_DOWNLOAD_URL}
          target="_blank"
          rel="noopener noreferrer"
          className="neon-notch neon-glow inline-flex w-full items-center justify-center bg-primary px-6 py-2.5 font-[family-name:var(--font-display)] text-[13px] font-bold tracking-[0.06em] text-primary-foreground transition-colors hover:bg-primary/90"
        >
          Descargar agente (Windows)
        </a>
        <Button
          variant="outline"
          size="lg"
          className="w-full font-[family-name:var(--font-display)] font-semibold tracking-[0.06em]"
          onClick={enter}
        >
          Saltar — entrar al estudio
        </Button>
      </div>
    </div>
  );
}

function Step({ n, children }: { n: number; children: React.ReactNode }) {
  return (
    <li className="flex items-start gap-3">
      <span className="mt-px grid size-5 shrink-0 place-items-center rounded-full bg-secondary font-[family-name:var(--font-mono)] text-[0.7rem] tabular-nums text-secondary-foreground">
        {n}
      </span>
      <span className="leading-relaxed">{children}</span>
    </li>
  );
}
