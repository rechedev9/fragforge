import { PairPcStep } from 'cs2video-web';

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
        width: 480,
      }}
    >
      {children}
    </div>
  );
}

export function Step() {
  return (
    <Frame>
      <PairPcStep />
    </Frame>
  );
}
