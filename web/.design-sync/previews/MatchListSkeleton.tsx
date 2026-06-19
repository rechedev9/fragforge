import { MatchListSkeleton } from 'cs2video-web';

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
        width: 780,
      }}
    >
      {children}
    </div>
  );
}

export function Loading() {
  return (
    <Frame>
      <MatchListSkeleton />
    </Frame>
  );
}
