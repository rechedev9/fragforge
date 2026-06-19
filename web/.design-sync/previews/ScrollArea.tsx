import { ScrollArea, Separator } from 'cs2video-web';

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
        width: 360,
      }}
    >
      {children}
    </div>
  );
}

const frags = [
  { round: 'R3', map: 'Mirage', kind: 'Ace', kills: 5 },
  { round: 'R7', map: 'Mirage', kind: '1v3 clutch', kills: 3 },
  { round: 'R11', map: 'Mirage', kind: 'Double kill', kills: 2 },
  { round: 'R14', map: 'Mirage', kind: 'AWP flick', kills: 1 },
  { round: 'R16', map: 'Mirage', kind: 'Triple', kills: 3 },
  { round: 'R19', map: 'Mirage', kind: 'Noscope', kills: 1 },
  { round: 'R22', map: 'Mirage', kind: '1v2 clutch', kills: 2 },
  { round: 'R25', map: 'Mirage', kind: 'Spray transfer', kills: 4 },
  { round: 'R27', map: 'Mirage', kind: 'Wallbang', kills: 1 },
];

export function FragFeed() {
  return (
    <Frame>
      <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
        <span style={{ fontSize: 11, letterSpacing: '0.08em', textTransform: 'uppercase', color: 'var(--muted-foreground)' }}>
          Highlight reel — 9 clips
        </span>
        <ScrollArea style={{ height: 180, borderRadius: 10, border: '1px solid var(--border)' }}>
          <div style={{ display: 'flex', flexDirection: 'column', padding: 8 }}>
            {frags.map((f, i) => (
              <div key={f.round}>
                <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', padding: '8px 6px' }}>
                  <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
                    <span style={{ fontFamily: 'var(--font-mono)', fontSize: 12, color: 'var(--muted-foreground)', width: 32 }}>
                      {f.round}
                    </span>
                    <span style={{ fontSize: 14 }}>{f.kind}</span>
                  </div>
                  <span style={{ fontFamily: 'var(--font-mono)', fontSize: 13, color: 'var(--primary)' }}>
                    {f.kills}K
                  </span>
                </div>
                {i < frags.length - 1 && <Separator />}
              </div>
            ))}
          </div>
        </ScrollArea>
      </div>
    </Frame>
  );
}
