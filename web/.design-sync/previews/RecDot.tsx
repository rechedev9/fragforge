import { RecDot, Wordmark } from 'cs2video-web';

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
        gap: 16,
      }}
    >
      {children}
    </div>
  );
}

export function CaptureBar() {
  return (
    <Frame>
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
          gap: 24,
          padding: '12px 16px',
          borderRadius: 10,
          background: 'var(--card)',
          border: '1px solid var(--border)',
          width: 420,
        }}
      >
        <Wordmark />
        <RecDot />
      </div>
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
          gap: 24,
          padding: '12px 16px',
          borderRadius: 10,
          background: 'var(--card)',
          border: '1px solid var(--border)',
          width: 420,
        }}
      >
        <span style={{ fontSize: 13, color: 'var(--muted-foreground)' }}>Mirage · recording</span>
        <RecDot label="REC 00:42" />
      </div>
    </Frame>
  );
}

export function DotOnly() {
  return (
    <Frame>
      <div style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
        <RecDot hideLabel />
        <span style={{ fontSize: 13, color: 'var(--muted-foreground)' }}>
          Capturing on your rig
        </span>
      </div>
    </Frame>
  );
}
