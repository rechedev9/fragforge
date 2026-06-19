import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuLabel,
  DropdownMenuItem,
  DropdownMenuSeparator,
  Button,
} from 'cs2video-web';
import { Download, Share2, Globe, Trash2 } from 'lucide-react';

function Stage({ children }: { children: React.ReactNode }) {
  return (
    <div
      className="dark"
      style={{
        background: 'var(--background)',
        color: 'var(--foreground)',
        fontFamily: 'var(--font-sans)',
        width: 420,
        minHeight: 420,
        display: 'flex',
        justifyContent: 'center',
        alignItems: 'flex-start',
        paddingTop: 28,
        boxSizing: 'border-box',
      }}
    >
      {children}
    </div>
  );
}

export function ReelActions() {
  return (
    <Stage>
      <DropdownMenu open>
        <DropdownMenuTrigger asChild>
          <Button variant="outline">Reel actions</Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="start">
          <DropdownMenuLabel>Mirage — Ace clutch</DropdownMenuLabel>
          <DropdownMenuSeparator />
          <DropdownMenuItem>
            <Download /> Download
          </DropdownMenuItem>
          <DropdownMenuItem>
            <Share2 /> Share link
          </DropdownMenuItem>
          <DropdownMenuItem>
            <Globe /> Publish to feed
          </DropdownMenuItem>
          <DropdownMenuSeparator />
          <DropdownMenuItem variant="destructive">
            <Trash2 /> Delete reel
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
    </Stage>
  );
}
