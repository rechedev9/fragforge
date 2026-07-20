'use client';

import { useSyncExternalStore, type ReactElement } from 'react';
import { ChevronsLeft, ChevronsRight, MessageCircle } from 'lucide-react';
import { AssistantPanel } from '@/components/assistant/assistant-panel';
import { AssistantProvider } from '@/components/assistant/assistant-provider';
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
 * The desktop rail can be collapsed to a slim strip via a toggle whose state
 * persists in localStorage (default: expanded); narrow layouts keep the
 * floating trigger plus Sheet drawer.
 */
const DESKTOP_ASSISTANT_QUERY = '(min-width: 1280px)';

const ASSISTANT_RAIL_STORAGE_KEY = 'fragforge.assistant.rail';
const RAIL_COLLAPSED_VALUE = 'collapsed';

export function AssistantRail(): ReactElement {
  const desktop = useSyncExternalStore(subscribeToDesktopLayout, desktopLayoutSnapshot, () => false);
  const collapsed = useSyncExternalStore(subscribeToRailState, railCollapsedSnapshot, () => false);
  let rail: ReactElement;
  if (!desktop) {
    rail = (
      <Sheet>
        <SheetTrigger asChild>
          <Button
            type="button"
            size="icon"
            className="fixed right-4 bottom-4 z-40 shadow-lg"
            aria-label="Abrir asistente"
            title="Abrir asistente"
          >
            <MessageCircle aria-hidden />
          </Button>
        </SheetTrigger>
        <SheetContent side="right" className="w-[min(100vw,27rem)] max-w-none gap-0 p-0 sm:max-w-none">
          <SheetHeader className="sr-only">
            <SheetTitle>Agente de FragForge</SheetTitle>
            <SheetDescription>Agente integrado capaz de operar los flujos de FragForge Studio.</SheetDescription>
          </SheetHeader>
          <AssistantPanel className="h-full min-h-0 border-0" />
        </SheetContent>
      </Sheet>
    );
  } else if (collapsed) {
    rail = (
      <aside className="flex w-12 shrink-0 flex-col items-center border-l border-border/80 bg-background/35 py-3">
        <Button
          type="button"
          variant="ghost"
          size="icon-xs"
          onClick={() => setRailCollapsed(false)}
          aria-label="Mostrar asistente"
          title="Mostrar asistente"
        >
          <ChevronsLeft aria-hidden />
        </Button>
      </aside>
    );
  } else {
    rail = (
      <aside className="flex w-[25rem] shrink-0 border-l border-border/80 bg-background/35 p-3">
        <div className="sticky top-3 mr-2 flex h-[calc(100vh-1.5rem)] shrink-0 flex-col">
          <Button
            type="button"
            variant="ghost"
            size="icon-xs"
            onClick={() => setRailCollapsed(true)}
            aria-label="Ocultar asistente"
            title="Ocultar asistente"
          >
            <ChevronsRight aria-hidden />
          </Button>
        </div>
        <AssistantPanel className="sticky top-3 h-[calc(100vh-1.5rem)] min-h-0 min-w-0 flex-1" />
      </aside>
    );
  }
  return <AssistantProvider>{rail}</AssistantProvider>;
}

function subscribeToDesktopLayout(onChange: () => void): () => void {
  const media = window.matchMedia(DESKTOP_ASSISTANT_QUERY);
  media.addEventListener('change', onChange);
  return () => media.removeEventListener('change', onChange);
}

function desktopLayoutSnapshot(): boolean {
  return window.matchMedia(DESKTOP_ASSISTANT_QUERY).matches;
}

const railListeners = new Set<() => void>();

function subscribeToRailState(onChange: () => void): () => void {
  railListeners.add(onChange);
  window.addEventListener('storage', onChange);
  return () => {
    railListeners.delete(onChange);
    window.removeEventListener('storage', onChange);
  };
}

function railCollapsedSnapshot(): boolean {
  try {
    return window.localStorage.getItem(ASSISTANT_RAIL_STORAGE_KEY) === RAIL_COLLAPSED_VALUE;
  } catch {
    return false;
  }
}

function setRailCollapsed(collapsed: boolean): void {
  try {
    if (collapsed) {
      window.localStorage.setItem(ASSISTANT_RAIL_STORAGE_KEY, RAIL_COLLAPSED_VALUE);
    } else {
      window.localStorage.removeItem(ASSISTANT_RAIL_STORAGE_KEY);
    }
  } catch {
    // Storage unavailable (private mode); the rail simply stays expanded.
  }
  for (const listener of railListeners) {
    listener();
  }
}
