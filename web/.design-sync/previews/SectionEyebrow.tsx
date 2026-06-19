import { SectionEyebrow } from 'cs2video-web';

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
        gap: 22,
        width: 360,
      }}
    >
      {children}
    </div>
  );
}

export function Sections() {
  return (
    <Frame>
      <div>
        <SectionEyebrow label="Forge highlights" count={4} />
        <div style={{ fontSize: 13, color: 'var(--muted-foreground)', marginTop: 8 }}>
          Mirage · Inferno · Nuke
        </div>
      </div>
      <div>
        <SectionEyebrow label="Rendering" count={2} />
        <div style={{ fontSize: 13, color: 'var(--muted-foreground)', marginTop: 8 }}>
          LIVE ON YOUR RIG
        </div>
      </div>
      <div>
        <SectionEyebrow label="Ready to post" count={6} />
        <div style={{ fontSize: 13, color: 'var(--muted-foreground)', marginTop: 8 }}>
          Reels in your library
        </div>
      </div>
    </Frame>
  );
}

export function NoCount() {
  return (
    <Frame>
      <SectionEyebrow label="Queued" />
      <SectionEyebrow label="Recording" />
      <SectionEyebrow label="Published" />
    </Frame>
  );
}
