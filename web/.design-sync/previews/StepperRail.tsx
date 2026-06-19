import { StepperRail } from 'cs2video-web';

function Frame({ children }: { children: React.ReactNode }) {
  return (
    <div
      className="dark"
      style={{
        background: 'var(--background)',
        color: 'var(--foreground)',
        fontFamily: 'var(--font-sans)',
        padding: 28,
        borderRadius: 14,
        border: '1px solid var(--border)',
        width: 240,
      }}
    >
      {children}
    </div>
  );
}

const steps = [
  { title: 'Link match history', hint: 'Paste your Steam auth code and sharecode.' },
  { title: 'Pair your PC', hint: 'Run the agent on your gaming rig.' },
  { title: 'Forge a reel', hint: 'Pick a highlight and a preset.' },
];

export function InProgress() {
  return (
    <Frame>
      <StepperRail steps={steps} current={1} />
    </Frame>
  );
}
