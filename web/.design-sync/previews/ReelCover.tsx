import { ReelCover } from 'cs2video-web';

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
        display: 'flex',
        gap: 18,
      }}
    >
      {children}
    </div>
  );
}

const covers = [
  { seed: 'mirage-16-12', label: 'Mirage' },
  { seed: 'inferno-banana-ace', label: 'Inferno' },
  { seed: 'nuke-ramp-hold', label: 'Nuke' },
  { seed: 'ancient-a-retake', label: 'Ancient' },
];

function Cover({ seed, label }: { seed: string; label: string }) {
  return (
    <div
      style={{
        width: 160,
        aspectRatio: '9 / 16',
        borderRadius: 12,
        overflow: 'hidden',
        border: '1px solid var(--border)',
      }}
    >
      <ReelCover seed={seed} label={label} />
    </div>
  );
}

export function Covers() {
  return (
    <Frame>
      {covers.slice(0, 3).map((c) => (
        <Cover key={c.seed} {...c} />
      ))}
    </Frame>
  );
}

export function Plain() {
  return (
    <Frame>
      <div
        style={{
          width: 160,
          aspectRatio: '9 / 16',
          borderRadius: 12,
          overflow: 'hidden',
          border: '1px solid var(--border)',
        }}
      >
        <ReelCover seed="dust2-long-pick" plain />
      </div>
      <Cover seed="ancient-a-retake" label="Ancient" />
    </Frame>
  );
}
