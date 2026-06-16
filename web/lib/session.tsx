'use client';

import { createContext, useCallback, useContext, useEffect, useState } from 'react';
import type { ReactNode } from 'react';
import { api } from '@/lib/api';
import type { Session } from '@/lib/api/types';

type SessionContextValue = {
  session: Session | null;
  loading: boolean;
  refresh: () => Promise<void>;
  signIn: () => Promise<void>;
  signOut: () => Promise<void>;
};

const SessionContext = createContext<SessionContextValue | null>(null);

export function SessionProvider({ children }: { children: ReactNode }) {
  const [session, setSession] = useState<Session | null>(null);
  const [loading, setLoading] = useState(true);

  const refresh = useCallback(async () => {
    const next = await api.getSession();
    setSession(next);
  }, []);

  const signIn = useCallback(async () => {
    const next = await api.signInWithSteam();
    setSession(next);
  }, []);

  const signOut = useCallback(async () => {
    await api.signOut();
    const next = await api.getSession();
    setSession(next);
  }, []);

  useEffect(() => {
    let active = true;
    (async () => {
      try {
        const next = await api.getSession();
        if (active) setSession(next);
      } finally {
        if (active) setLoading(false);
      }
    })();
    return () => {
      active = false;
    };
  }, []);

  return (
    <SessionContext.Provider value={{ session, loading, refresh, signIn, signOut }}>
      {children}
    </SessionContext.Provider>
  );
}

export function useSession(): SessionContextValue {
  const ctx = useContext(SessionContext);
  if (!ctx) {
    throw new Error('useSession must be used within a SessionProvider');
  }
  return ctx;
}
