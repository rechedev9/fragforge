// design-sync shim for `@/lib/session` — the real useSession() throws unless an
// ancestor SessionProvider is mounted, which preview cards can't supply (the
// context is module-private). Return a fixed signed-in session so leaf
// components (AppSidebar, LinkHistoryStep, PairPcStep) render standalone.
import type { Session } from '@/lib/api/types';

const session: Session = {
  user: { id: '1', personaName: 'shroud_btw', avatarUrl: '' },
  slots: { used: 3, total: 5 },
  pcPaired: true,
  matchHistoryLinked: true,
};

const noop = async () => {};

export function useSession() {
  return { session, loading: false, refresh: noop, signIn: noop, signOut: noop };
}

// Passthrough — components that wrap in it still work; useSession ignores it.
export function SessionProvider({ children }: { children: unknown }) {
  return children as never;
}
