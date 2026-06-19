// design-sync shim for `next-themes` — the app is forced dark, so report a
// fixed dark theme and pass children through. DS components (e.g. the sonner
// Toaster) read useTheme() to pick a variant.
import * as React from 'react';

export function useTheme() {
  return {
    theme: 'dark',
    setTheme: (_theme: string) => {},
    resolvedTheme: 'dark',
    systemTheme: 'dark' as const,
    themes: ['light', 'dark'],
    forcedTheme: undefined as string | undefined,
  };
}

export function ThemeProvider({ children }: { children?: React.ReactNode }) {
  return <>{children}</>;
}
