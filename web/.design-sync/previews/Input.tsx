import { Input, Label } from 'cs2video-web';

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
        width: 360,
      }}
    >
      {children}
    </div>
  );
}

export function AuthCode() {
  return (
    <Frame>
      <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
        <Label htmlFor="authcode">Steam auth code</Label>
        <Input
          id="authcode"
          placeholder="ABCD-EFGHI-JKLMN"
          defaultValue="7K2P-9QZ4M-X8B1N"
          style={{ fontFamily: 'var(--font-mono)', letterSpacing: '0.06em' }}
        />
        <span style={{ fontSize: 12, color: 'var(--muted-foreground)' }}>
          Found in Steam → Game Records → CS2.
        </span>
      </div>
    </Frame>
  );
}

export function SearchAndShare() {
  return (
    <Frame>
      <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
        <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
          <Label htmlFor="search">Find a match</Label>
          <Input id="search" type="search" placeholder="Search Mirage, Inferno, Nuke…" />
        </div>
        <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
          <Label htmlFor="sharecode">Match sharecode</Label>
          <Input
            id="sharecode"
            placeholder="CSGO-xxxxx-xxxxx-xxxxx-xxxxx-xxxxx"
            aria-invalid
            defaultValue="CSGO-9aBc2-dEf3G"
            style={{ fontFamily: 'var(--font-mono)' }}
          />
          <span style={{ fontSize: 12, color: 'var(--destructive)' }}>
            Sharecode looks incomplete.
          </span>
        </div>
      </div>
    </Frame>
  );
}
