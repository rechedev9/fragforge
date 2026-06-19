import { Progress } from 'cs2video-web';

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
        width: 380,
      }}
    >
      {children}
    </div>
  );
}

function Row({ label, value }: { label: string; value: number }) {
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: 13 }}>
        <span style={{ color: 'var(--muted-foreground)' }}>{label}</span>
        <span style={{ fontFamily: 'var(--font-mono)', color: 'var(--primary)' }}>{value}%</span>
      </div>
      <Progress value={value} />
    </div>
  );
}

export function RenderQueue() {
  return (
    <Frame>
      <div style={{ display: 'flex', flexDirection: 'column', gap: 18 }}>
        <Row label="Parsing demo" value={100} />
        <Row label="Recording highlights" value={72} />
        <Row label="Composing reel" value={35} />
        <Row label="Publishing" value={8} />
      </div>
    </Frame>
  );
}

export function UploadProgress() {
  return (
    <Frame>
      <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'baseline' }}>
          <span style={{ fontWeight: 600 }}>Uploading de_mirage_2026.dem</span>
          <span style={{ fontFamily: 'var(--font-mono)', fontSize: 13, color: 'var(--muted-foreground)' }}>
            58 / 92 MB
          </span>
        </div>
        <Progress value={63} />
      </div>
    </Frame>
  );
}
