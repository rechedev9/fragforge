'use client';

import { createContext, useContext, useEffect, useState, type ReactElement, type ReactNode } from 'react';
import {
  applyAssistantEvent,
  ASSISTANT_AVAILABILITY,
  beginAssistantCommand,
  finishAssistantCommand,
  getFragforgeAssistantBridge,
  initialAssistantSnapshot,
  parseAssistantCommandResult,
  parseAssistantSnapshotEvent,
  type AssistantCommandResult,
  type AssistantCommandState,
  type FragforgeAssistantBridge,
} from '@/lib/assistant';

const EMPTY_UNAVAILABLE_ERROR = 'El asistente integrado solo está disponible en FragForge Studio para Windows.';

interface AssistantProviderValue extends AssistantCommandState {
  bridge: FragforgeAssistantBridge | null;
  cancelPending: boolean;
  draft: string;
  runCommand(
    invoke: (bridge: FragforgeAssistantBridge) => Promise<AssistantCommandResult>,
    fallbackError: string,
  ): Promise<boolean>;
  setCancelPending(value: boolean): void;
  setDraft(value: string): void;
}

const AssistantContext = createContext<AssistantProviderValue | null>(null);

export function AssistantProvider({ children }: { children: ReactNode }): ReactElement {
  const [bridge, setBridge] = useState<FragforgeAssistantBridge | null>(null);
  const [cancelPending, setCancelPending] = useState(false);
  const [draft, setDraft] = useState('');
  const [state, setState] = useState<AssistantCommandState>(() => ({
    commandPendingCount: 0,
    snapshot: initialAssistantSnapshot(),
  }));

  useEffect(() => {
    const nextBridge = getFragforgeAssistantBridge();
    if (nextBridge === null) {
      setBridge(null);
      setState({
        commandPendingCount: 0,
        snapshot: {
          ...initialAssistantSnapshot(ASSISTANT_AVAILABILITY.unavailable),
          error: EMPTY_UNAVAILABLE_ERROR,
        },
      });
      return;
    }

    let mounted = true;
    let unsubscribe = (): void => {};
    try {
      unsubscribe = nextBridge.subscribe((eventValue) => {
        if (!mounted) return;
        try {
          const event = parseAssistantSnapshotEvent(eventValue);
          setState((previous) => ({
            ...previous,
            snapshot: applyAssistantEvent(previous.snapshot, event),
          }));
        } catch {
          setState((previous) => ({
            ...previous,
            controlError: 'Studio recibió un estado de Codex no válido.',
          }));
        }
      });
    } catch {
      setBridge(null);
      setState({
        commandPendingCount: 0,
        snapshot: {
          ...initialAssistantSnapshot(ASSISTANT_AVAILABILITY.error),
          error: 'No se pudo conectar el panel con Codex.',
        },
      });
      return;
    }
    void nextBridge.status()
      .then((value) => {
        if (!mounted) return;
        const result = parseAssistantCommandResult(value);
        setBridge(result.ok ? nextBridge : null);
        if (!result.ok) {
          unsubscribe();
          unsubscribe = (): void => {};
        }
        setState((previous) => {
          if (result.ok) {
            return {
              ...previous,
              snapshot: applyAssistantEvent(previous.snapshot, { snapshot: result.snapshot, type: 'snapshot' }),
            };
          }
          const snapshot = result.snapshot === undefined
            ? {
                ...previous.snapshot,
                availability: ASSISTANT_AVAILABILITY.error,
                error: result.error,
              }
            : applyAssistantEvent(previous.snapshot, { snapshot: result.snapshot, type: 'snapshot' });
          return {
            ...previous,
            controlError: result.error,
            snapshot,
          };
        });
      })
      .catch(() => {
        if (!mounted) return;
        unsubscribe();
        unsubscribe = (): void => {};
        setBridge(null);
        setState((previous) => ({
          ...previous,
          controlError: 'No se pudo iniciar la conversación de Codex.',
          snapshot: {
            ...previous.snapshot,
            availability: ASSISTANT_AVAILABILITY.error,
            error: 'No se pudo iniciar la conversación de Codex.',
          },
        }));
      });

    return () => {
      mounted = false;
      unsubscribe();
    };
  }, []);

  async function runCommand(
    invoke: (activeBridge: FragforgeAssistantBridge) => Promise<AssistantCommandResult>,
    fallbackError: string,
  ): Promise<boolean> {
    if (bridge === null) return false;
    setState((previous) => beginAssistantCommand(previous));
    try {
      const result = parseAssistantCommandResult(await invoke(bridge));
      setState((previous) => finishAssistantCommand(previous, result));
      return result.ok;
    } catch {
      setState((previous) => finishAssistantCommand(previous, { error: fallbackError, ok: false }));
      return false;
    }
  }

  return (
    <AssistantContext.Provider value={{
      ...state,
      bridge,
      cancelPending,
      draft,
      runCommand,
      setCancelPending,
      setDraft,
    }}>
      {children}
    </AssistantContext.Provider>
  );
}

export function useAssistantContext(): AssistantProviderValue {
  const value = useContext(AssistantContext);
  if (value === null) throw new Error('AssistantPanel must be rendered inside AssistantProvider');
  return value;
}
