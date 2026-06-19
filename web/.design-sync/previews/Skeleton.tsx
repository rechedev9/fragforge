import { Skeleton } from 'cs2video-web';

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

export function MatchCardLoading() {
  return (
    <Frame>
      <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 14 }}>
          <Skeleton style={{ width: 56, height: 56, borderRadius: 12 }} />
          <div style={{ display: 'flex', flexDirection: 'column', gap: 8, flex: 1 }}>
            <Skeleton style={{ width: '55%', height: 16 }} />
            <Skeleton style={{ width: '35%', height: 12 }} />
          </div>
          <Skeleton style={{ width: 64, height: 28, borderRadius: 999 }} />
        </div>
        <Skeleton style={{ width: '100%', height: 1 }} />
        <div style={{ display: 'flex', gap: 22 }}>
          <Skeleton style={{ width: 48, height: 36, borderRadius: 8 }} />
          <Skeleton style={{ width: 48, height: 36, borderRadius: 8 }} />
          <Skeleton style={{ width: 48, height: 36, borderRadius: 8 }} />
          <Skeleton style={{ width: 48, height: 36, borderRadius: 8 }} />
        </div>
      </div>
    </Frame>
  );
}

export function ClipThumbsLoading() {
  return (
    <Frame>
      <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
        <Skeleton style={{ width: 140, height: 14 }} />
        <div style={{ display: 'flex', gap: 12 }}>
          {[0, 1, 2].map((i) => (
            <div key={i} style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
              <Skeleton style={{ width: 116, height: 66, borderRadius: 10 }} />
              <Skeleton style={{ width: 80, height: 10 }} />
            </div>
          ))}
        </div>
      </div>
    </Frame>
  );
}
