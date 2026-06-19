import { Wordmark } from 'cs2video-web';

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
        flexDirection: 'column',
        gap: 24,
        alignItems: 'flex-start',
      }}
    >
      {children}
    </div>
  );
}

export function Brand() {
  return (
    <Frame>
      <div style={{ transform: 'scale(1.6)', transformOrigin: 'left center' }}>
        <Wordmark />
      </div>
      <Wordmark />
      <div style={{ transform: 'scale(0.85)', transformOrigin: 'left center' }}>
        <Wordmark />
      </div>
    </Frame>
  );
}

export function TextOnly() {
  return (
    <Frame>
      <Wordmark hideMark />
      <span style={{ fontSize: 13, color: 'var(--muted-foreground)' }}>
        Lime “Frag” + white “Forge” — the FragForge mark
      </span>
    </Frame>
  );
}
