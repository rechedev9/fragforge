import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
  SheetDescription,
  SheetFooter,
  Button,
} from 'cs2video-web';

function Stage({ children }: { children: React.ReactNode }) {
  return (
    <div
      className="dark"
      style={{
        position: 'fixed',
        inset: 0,
        background: 'var(--background)',
        color: 'var(--foreground)',
        fontFamily: 'var(--font-sans)',
      }}
    >
      {children}
    </div>
  );
}

export function RightPanel() {
  return (
    <Stage>
      <Sheet open>
        <SheetContent side="right">
          <SheetHeader>
            <SheetTitle className="font-[family-name:var(--font-display)] tracking-tight">
              Reel settings
            </SheetTitle>
            <SheetDescription>Tune how this highlight is captured and cut.</SheetDescription>
          </SheetHeader>
          <div style={{ display: 'flex', flexDirection: 'column', gap: 10, padding: '0 16px' }}>
            <span className="text-sm text-foreground">Clean POV</span>
            <span className="text-sm text-muted-foreground">Music edit</span>
            <span className="text-sm text-muted-foreground">Show kill feed</span>
          </div>
          <SheetFooter>
            <Button>Save</Button>
          </SheetFooter>
        </SheetContent>
      </Sheet>
    </Stage>
  );
}
