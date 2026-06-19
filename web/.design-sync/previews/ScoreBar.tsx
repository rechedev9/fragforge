import { ScoreBar } from 'cs2video-web';

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
        gap: 10,
        width: 360,
      }}
    >
      {children}
    </div>
  );
}

function Row({ win, map, score }: { win: boolean; map: string; score: string }) {
  return (
    <div
      style={{
        display: 'flex',
        alignItems: 'stretch',
        gap: 12,
        padding: 12,
        borderRadius: 10,
        background: 'var(--card)',
        border: '1px solid var(--border)',
        minHeight: 52,
      }}
    >
      <ScoreBar win={win} />
      <div
        style={{
          display: 'flex',
          flexDirection: 'column',
          justifyContent: 'center',
          gap: 2,
        }}
      >
        <span style={{ fontSize: 14, fontWeight: 600 }}>{map}</span>
        <span
          style={{
            fontFamily: 'var(--font-mono)',
            fontSize: 12,
            color: win ? 'var(--primary)' : 'var(--muted-foreground)',
          }}
        >
          {win ? 'WIN' : 'LOSS'} · {score}
        </span>
      </div>
    </div>
  );
}

export function WinLoss() {
  return (
    <Frame>
      <Row win map="Mirage" score="16-12" />
      <Row win map="Nuke" score="16-9" />
      <Row win={false} map="Inferno" score="11-16" />
      <Row win={false} map="Dust2" score="13-16" />
    </Frame>
  );
}
