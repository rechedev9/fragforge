import { RenderingCard } from 'cs2video-web';

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
        width: 420,
      }}
    >
      {children}
    </div>
  );
}

const recording = {
  id: 'rec-nuke-ramp',
  title: 'Nuke ramp triple frag',
  map: 'Nuke',
  score: '10-7',
  mode: 'music' as const,
  status: 'recording' as const,
  createdAt: Date.now() - 1000 * 60 * 2,
  published: false,
};

const queued = {
  id: 'rec-ancient-retake',
  title: 'Ancient B retake 4K',
  map: 'Ancient',
  score: '13-11',
  mode: 'clean' as const,
  status: 'queued' as const,
  createdAt: Date.now() - 1000 * 30,
  published: false,
};

const composing = {
  id: 'rec-anubis-ot',
  title: 'Anubis overtime ace',
  map: 'Anubis',
  score: '15-15',
  mode: 'music' as const,
  status: 'composing' as const,
  createdAt: Date.now() - 1000 * 60 * 4,
  published: false,
};

export function Recording() {
  return (
    <Frame>
      <RenderingCard video={recording} />
    </Frame>
  );
}

export function Queued() {
  return (
    <Frame>
      <RenderingCard video={queued} />
    </Frame>
  );
}

export function Composing() {
  return (
    <Frame>
      <RenderingCard video={composing} />
    </Frame>
  );
}
