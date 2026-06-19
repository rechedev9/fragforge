import { StatMono } from 'cs2video-web';

// Dark studio surface — the app is forced-dark, so every card renders on the
// charcoal background with the brand fonts, matching the real product.
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
      }}
    >
      {children}
    </div>
  );
}

export function Scoreboard() {
  return (
    <Frame>
      <div style={{ display: 'flex', gap: 28, alignItems: 'flex-end' }}>
        <StatMono label="K" value={31} />
        <StatMono label="D" value={14} />
        <StatMono label="A" value={7} />
        <StatMono label="MVP" value={5} />
        <StatMono label="K/D" value="2.21" accent />
      </div>
    </Frame>
  );
}

export function Inline() {
  return (
    <Frame>
      <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
        <StatMono layout="inline" label="ADR" value="118.4" />
        <StatMono layout="inline" label="HS%" value="61%" accent />
        <StatMono layout="inline" label="Tick" value={128} />
      </div>
    </Frame>
  );
}
