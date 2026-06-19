import {
  Card,
  CardHeader,
  CardTitle,
  CardDescription,
  CardContent,
  CardFooter,
  CardAction,
  Badge,
  Button,
  StatMono,
} from 'cs2video-web';
import { Globe, Play } from 'lucide-react';

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
        width: 420,
      }}
    >
      {children}
    </div>
  );
}

export function ReelSummary() {
  return (
    <Frame>
      <Card>
        <CardHeader>
          <CardTitle className="font-[family-name:var(--font-display)] tracking-tight">
            Mirage — Ace clutch
          </CardTitle>
          <CardDescription>Music edit · 0:42 · 9:16 short</CardDescription>
          <CardAction>
            <Badge>
              <Globe /> Ready
            </Badge>
          </CardAction>
        </CardHeader>
        <CardContent>
          <div style={{ display: 'flex', gap: 24 }}>
            <StatMono label="K" value={5} />
            <StatMono label="Round" value={24} />
            <StatMono label="HS%" value="80%" accent />
          </div>
        </CardContent>
        <CardFooter className="justify-between">
          <span className="font-[family-name:var(--font-mono)] text-sm tabular-nums text-muted-foreground">
            expires in 13h
          </span>
          <Button size="sm">
            <Play /> View reel
          </Button>
        </CardFooter>
      </Card>
    </Frame>
  );
}
