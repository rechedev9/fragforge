import { Avatar, AvatarImage, AvatarFallback } from 'cs2video-web';

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
        alignItems: 'center',
        gap: 16,
      }}
    >
      {children}
    </div>
  );
}

export function Sizes() {
  return (
    <Frame>
      <Avatar size="sm">
        <AvatarImage src="" alt="s1mple" />
        <AvatarFallback>S1</AvatarFallback>
      </Avatar>
      <Avatar>
        <AvatarImage src="" alt="ZywOo" />
        <AvatarFallback>ZY</AvatarFallback>
      </Avatar>
      <Avatar size="lg">
        <AvatarImage src="" alt="device" />
        <AvatarFallback>dev</AvatarFallback>
      </Avatar>
    </Frame>
  );
}

export function Roster() {
  return (
    <Frame>
      <Avatar>
        <AvatarImage src="" alt="m0NESY" />
        <AvatarFallback>M0</AvatarFallback>
      </Avatar>
      <Avatar>
        <AvatarImage src="" alt="NiKo" />
        <AvatarFallback>NK</AvatarFallback>
      </Avatar>
      <Avatar>
        <AvatarImage src="" alt="huNter" />
        <AvatarFallback>HU</AvatarFallback>
      </Avatar>
      <Avatar>
        <AvatarImage src="" alt="AWP" />
        <AvatarFallback>AWP</AvatarFallback>
      </Avatar>
    </Frame>
  );
}
