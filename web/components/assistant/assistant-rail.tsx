import { MessageCircle } from 'lucide-react';
import { AssistantPanel } from '@/components/assistant/assistant-panel';
import { Button } from '@/components/ui/button';
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
  SheetTrigger,
} from '@/components/ui/sheet';

/**
 * Keeps the assistant present as a desktop rail and reachable as a right
 * drawer when the Studio window is too narrow to reserve permanent space.
 */
export function AssistantRail() {
  return (
    <>
      <aside className="hidden w-[23rem] shrink-0 border-l border-border/80 bg-background/35 p-3 xl:block">
        <AssistantPanel className="sticky top-3 h-[calc(100vh-1.5rem)] min-h-0" />
      </aside>

      <Sheet>
        <SheetTrigger asChild>
          <Button
            type="button"
            size="icon"
            className="fixed right-4 bottom-4 z-40 shadow-lg xl:hidden"
            aria-label="Abrir asistente"
            title="Abrir asistente"
          >
            <MessageCircle aria-hidden />
          </Button>
        </SheetTrigger>
        <SheetContent side="right" className="w-[min(100vw,27rem)] max-w-none gap-0 p-0 sm:max-w-none">
          <SheetHeader className="sr-only">
            <SheetTitle>Asistente de Codex</SheetTitle>
            <SheetDescription>Chat integrado de FragForge Studio.</SheetDescription>
          </SheetHeader>
          <AssistantPanel className="h-full min-h-0 border-0" />
        </SheetContent>
      </Sheet>
    </>
  );
}
