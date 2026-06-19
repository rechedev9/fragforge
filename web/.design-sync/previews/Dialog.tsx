import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
  Button,
} from 'cs2video-web';

// Full-bleed dark stage so the radix overlay scrim sits on charcoal, not the
// emitter's white body. cardMode:"single" (config) makes this the containing
// block for the portal'd, position:fixed dialog.
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

export function ConfirmDelete() {
  return (
    <Stage>
      <Dialog open>
        <DialogContent>
          <DialogHeader>
            <DialogTitle className="font-[family-name:var(--font-display)] tracking-tight">
              Delete this reel?
            </DialogTitle>
            <DialogDescription>
              This removes the rendered short from your library. Your demo stays on your rig.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline">Cancel</Button>
            <Button variant="destructive">Delete reel</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </Stage>
  );
}
