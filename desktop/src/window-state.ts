// Pure validation of the persisted window geometry (window.json). Split out of
// loadWindowState() so the finite/size-sanity checks can be unit tested without
// touching the filesystem. No side effects, no relative imports, so `node --test`
// runs this .ts file directly.

export interface WindowBounds {
  width: number;
  height: number;
  x?: number;
  y?: number;
}

export interface WindowState {
  bounds: WindowBounds;
  isMaximized: boolean;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null;
}

// Same defaults the inline version used: a comfortable windowed size, not
// maximized. Returned as a fresh object each call so callers can never mutate a
// shared fallback.
function fallback(): WindowState {
  return { bounds: { width: 1280, height: 900 }, isMaximized: false };
}

/**
 * Validates an already-JSON.parse'd value read from window.json and returns
 * usable bounds plus the maximize flag, or the fallback if the input is
 * missing, corrupt, the wrong shape, has non-finite dimensions, or is
 * implausibly small. x/y are only carried over when both are finite numbers.
 */
export function validateWindowState(saved: unknown): WindowState {
  if (!isRecord(saved)) return fallback();
  const { width, height, x, y, isMaximized } = saved;
  if (
    typeof width !== 'number' ||
    typeof height !== 'number' ||
    !Number.isFinite(width) ||
    !Number.isFinite(height) ||
    width < 800 ||
    height < 600
  ) {
    return fallback();
  }
  const bounds: WindowBounds = { width, height };
  if (typeof x === 'number' && typeof y === 'number' && Number.isFinite(x) && Number.isFinite(y)) {
    bounds.x = x;
    bounds.y = y;
  }
  return { bounds, isMaximized: isMaximized === true };
}
