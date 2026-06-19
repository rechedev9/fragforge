import { ReadyCard } from 'cs2video-web';

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
        width: 340,
      }}
    >
      {children}
    </div>
  );
}

const unpublished = {
  id: 'rdy-mirage-ace',
  title: 'Mirage A-site retake ace',
  map: 'Mirage',
  score: '16-12',
  mode: 'music' as const,
  status: 'ready' as const,
  createdAt: Date.now() - 1000 * 60 * 18,
  availableForSec: 60 * 60 * 22 + 60 * 14,
  published: false,
  downloadUrl: '/reel-sample.mp4',
};

const published = {
  id: 'rdy-inferno-clutch',
  title: 'Inferno 1v3 banana clutch',
  map: 'Inferno',
  score: '13-9',
  mode: 'clean' as const,
  status: 'ready' as const,
  createdAt: Date.now() - 1000 * 60 * 60 * 5,
  availableForSec: 60 * 60 * 6 + 60 * 41,
  published: true,
  downloadUrl: '/reel-sample.mp4',
};

export function Unpublished() {
  return (
    <Frame>
      <ReadyCard video={unpublished} />
    </Frame>
  );
}

export function Published() {
  return (
    <Frame>
      <ReadyCard video={published} />
    </Frame>
  );
}
