import {
  Avatar,
  AvatarImage,
  AvatarFallback,
  AvatarGroup,
  AvatarGroupCount,
} from 'cs2video-web';

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
        gap: 18,
        alignItems: 'flex-start',
      }}
    >
      {children}
    </div>
  );
}

export function Lineup() {
  return (
    <Frame>
      <AvatarGroup>
        <Avatar>
          <AvatarImage src="" alt="s1mple" />
          <AvatarFallback>S1</AvatarFallback>
        </Avatar>
        <Avatar>
          <AvatarImage src="" alt="ZywOo" />
          <AvatarFallback>ZY</AvatarFallback>
        </Avatar>
        <Avatar>
          <AvatarImage src="" alt="m0NESY" />
          <AvatarFallback>M0</AvatarFallback>
        </Avatar>
        <AvatarGroupCount>+7</AvatarGroupCount>
      </AvatarGroup>
    </Frame>
  );
}

export function Sizes() {
  return (
    <Frame>
      <AvatarGroup>
        <Avatar size="sm">
          <AvatarImage src="" alt="NiKo" />
          <AvatarFallback>NK</AvatarFallback>
        </Avatar>
        <Avatar size="sm">
          <AvatarImage src="" alt="huNter" />
          <AvatarFallback>HU</AvatarFallback>
        </Avatar>
        <AvatarGroupCount>+3</AvatarGroupCount>
      </AvatarGroup>
      <AvatarGroup>
        <Avatar size="lg">
          <AvatarImage src="" alt="device" />
          <AvatarFallback>dev</AvatarFallback>
        </Avatar>
        <Avatar size="lg">
          <AvatarImage src="" alt="dupreeh" />
          <AvatarFallback>DU</AvatarFallback>
        </Avatar>
        <Avatar size="lg">
          <AvatarImage src="" alt="Magisk" />
          <AvatarFallback>MG</AvatarFallback>
        </Avatar>
        <AvatarGroupCount>+12</AvatarGroupCount>
      </AvatarGroup>
    </Frame>
  );
}
