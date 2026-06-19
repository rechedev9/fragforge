// design-sync shim for `next/navigation` — no-op router/hooks so DS components
// that read navigation state render statically outside Next's runtime.
const noop = () => {};

export function useRouter() {
  return {
    push: noop,
    replace: noop,
    prefetch: noop,
    back: noop,
    forward: noop,
    refresh: noop,
  };
}

export function usePathname(): string {
  return '/';
}

export function useSearchParams(): URLSearchParams {
  return new URLSearchParams();
}

export function useParams(): Record<string, string> {
  return {};
}

export function useSelectedLayoutSegment(): string | null {
  return null;
}

export function useSelectedLayoutSegments(): string[] {
  return [];
}

export function redirect(): never {
  throw new Error('next/navigation redirect() is a no-op in the design-sync shim');
}

export function notFound(): never {
  throw new Error('next/navigation notFound() is a no-op in the design-sync shim');
}
