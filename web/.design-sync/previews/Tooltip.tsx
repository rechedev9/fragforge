import { TooltipProvider, Tooltip, TooltipTrigger, TooltipContent, Button } from 'cs2video-web';

function Stage({ children }: { children: React.ReactNode }) {
  return (
    <div
      className="dark"
      style={{
        background: 'var(--background)',
        color: 'var(--foreground)',
        fontFamily: 'var(--font-sans)',
        width: 420,
        minHeight: 220,
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        boxSizing: 'border-box',
      }}
    >
      {children}
    </div>
  );
}

export function OnHover() {
  return (
    <Stage>
      <TooltipProvider>
        <Tooltip open>
          <TooltipTrigger asChild>
            <Button variant="secondary">LIVE ON YOUR RIG</Button>
          </TooltipTrigger>
          <TooltipContent side="bottom">Capturing on your GPU — RTX 4070</TooltipContent>
        </Tooltip>
      </TooltipProvider>
    </Stage>
  );
}
