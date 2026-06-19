import { GrainOverlay, Wordmark } from 'cs2video-web';

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

// GrainOverlay is a fixed, full-viewport faint film-grain field. To make the
// texture visible inside a card, we scope it to a sized panel via a contained
// stacking context (position:relative + overflow:hidden) and lay realistic
// content behind it so the grain reads as a tape/replay overlay.
export function Texture() {
  return (
    <Frame>
      <div
        style={{
          position: 'relative',
          width: 420,
          height: 220,
          borderRadius: 12,
          overflow: 'hidden',
          background:
            'radial-gradient(120% 90% at 75% 20%, hsl(150 45% 14%) 0%, hsl(170 40% 8%) 45%, #0b0c0e 80%)',
          border: '1px solid var(--border)',
        }}
      >
        <div
          style={{
            position: 'absolute',
            inset: 0,
            display: 'flex',
            flexDirection: 'column',
            justifyContent: 'space-between',
            padding: 22,
          }}
        >
          <Wordmark />
          <div>
            <div
              style={{
                fontFamily: 'var(--font-display)',
                fontSize: 26,
                fontWeight: 700,
                letterSpacing: '-0.01em',
              }}
            >
              Mirage · 16-12
            </div>
            <div
              style={{
                fontFamily: 'var(--font-mono)',
                fontSize: 12,
                letterSpacing: '0.16em',
                textTransform: 'uppercase',
                color: 'var(--primary)',
                marginTop: 6,
              }}
            >
              Forge highlights
            </div>
          </div>
        </div>
        {/* Scope the fixed grain layer to this panel's box. */}
        <div
          style={{
            position: 'absolute',
            inset: 0,
            contain: 'paint',
          }}
        >
          <GrainOverlay />
        </div>
      </div>
    </Frame>
  );
}
