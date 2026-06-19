import { FailedCard } from 'cs2video-web';

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

const recordFailed = {
  id: 'fail-overpass-rec',
  title: 'Overpass A long pick',
  map: 'Overpass',
  score: '8-13',
  mode: 'clean' as const,
  status: 'failed' as const,
  createdAt: Date.now() - 1000 * 60 * 12,
  published: false,
  failureReason: 'CS2 crashed on your rig mid-capture. Retry to re-record this clip.',
};

const renderFailed = {
  id: 'fail-dust2-render',
  title: 'Dust2 mid doubles',
  map: 'Dust2',
  score: '16-14',
  mode: 'music' as const,
  status: 'failed' as const,
  createdAt: Date.now() - 1000 * 60 * 40,
  published: false,
  failureReason: 'The render stage timed out while compositing overlays.',
};

export function RecordFailed() {
  return (
    <Frame>
      <FailedCard video={recordFailed} onChange={() => {}} />
    </Frame>
  );
}

export function RenderFailed() {
  return (
    <Frame>
      <FailedCard video={renderFailed} onChange={() => {}} />
    </Frame>
  );
}
