import { CreateReelBar } from 'cs2video-web';

function Frame({ children }: { children: React.ReactNode }) {
  return (
    <div
      className="dark"
      style={{
        background: 'var(--background)',
        color: 'var(--foreground)',
        fontFamily: 'var(--font-sans)',
        padding: 24,
        borderRadius: 14,
        border: '1px solid var(--border)',
        display: 'flex',
        flexDirection: 'column',
        gap: 20,
        width: 600,
      }}
    >
      {children}
    </div>
  );
}

export function Ready() {
  return (
    <Frame>
      <CreateReelBar
        playLabel="4K mid retake"
        presetLabel="Music edit"
        songTitle="Phonk Drift"
        creating={false}
        onCreate={() => {}}
      />
    </Frame>
  );
}

export function Empty() {
  return (
    <Frame>
      <CreateReelBar
        playLabel={null}
        presetLabel={null}
        songTitle={null}
        creating={false}
        onCreate={() => {}}
      />
    </Frame>
  );
}
