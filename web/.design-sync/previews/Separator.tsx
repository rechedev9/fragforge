import { Separator } from 'cs2video-web';

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
        width: 420,
      }}
    >
      {children}
    </div>
  );
}

function Stat({ label, value, accent }: { label: string; value: string; accent?: boolean }) {
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
      <span style={{ fontSize: 11, letterSpacing: '0.08em', textTransform: 'uppercase', color: 'var(--muted-foreground)' }}>
        {label}
      </span>
      <span
        style={{
          fontFamily: 'var(--font-mono)',
          fontSize: 22,
          fontWeight: 600,
          color: accent ? 'var(--primary)' : 'var(--foreground)',
        }}
      >
        {value}
      </span>
    </div>
  );
}

export function VerticalStats() {
  return (
    <Frame>
      <div style={{ display: 'flex', alignItems: 'center', gap: 20 }}>
        <Stat label="K/D" value="2.21" accent />
        <Separator orientation="vertical" style={{ height: 40 }} />
        <Stat label="ADR" value="118.4" />
        <Separator orientation="vertical" style={{ height: 40 }} />
        <Stat label="HS%" value="61%" />
        <Separator orientation="vertical" style={{ height: 40 }} />
        <Stat label="Clips" value="14" accent />
      </div>
    </Frame>
  );
}

export function HorizontalSections() {
  return (
    <Frame>
      <div style={{ display: 'flex', flexDirection: 'column', gap: 14 }}>
        <div style={{ display: 'flex', justifyContent: 'space-between' }}>
          <span style={{ fontWeight: 600 }}>Mirage</span>
          <span style={{ fontFamily: 'var(--font-mono)', color: 'var(--primary)' }}>16-12</span>
        </div>
        <Separator />
        <div style={{ display: 'flex', justifyContent: 'space-between', color: 'var(--muted-foreground)', fontSize: 13 }}>
          <span>Premier · 3h ago</span>
          <span style={{ fontFamily: 'var(--font-mono)' }}>128 tick</span>
        </div>
        <Separator />
        <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: 13 }}>
          <span>4 highlights queued</span>
          <span style={{ color: 'var(--primary)' }}>Render ready</span>
        </div>
      </div>
    </Frame>
  );
}
