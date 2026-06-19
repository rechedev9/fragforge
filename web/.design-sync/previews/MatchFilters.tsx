import { MatchFilters } from 'cs2video-web';

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
        width: 720,
      }}
    >
      {children}
    </div>
  );
}

export function AllSelected() {
  return (
    <Frame>
      <MatchFilters
        filter="all"
        onFilterChange={() => {}}
        query=""
        onQueryChange={() => {}}
      />
    </Frame>
  );
}

export function WinsWithSearch() {
  return (
    <Frame>
      <MatchFilters
        filter="wins"
        onFilterChange={() => {}}
        query="Mirage"
        onQueryChange={() => {}}
      />
    </Frame>
  );
}

export function BestFrags() {
  return (
    <Frame>
      <MatchFilters
        filter="frags"
        onFilterChange={() => {}}
        query=""
        onQueryChange={() => {}}
      />
    </Frame>
  );
}
