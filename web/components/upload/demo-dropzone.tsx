'use client';

import { useCallback, useRef, useState } from 'react';
import { UploadCloud } from 'lucide-react';
import { cn } from '@/lib/utils';

export type DemoDropzoneProps = {
  /** Called with the chosen .dem file. The parent owns parsing + navigation. */
  onFile: (file: File) => void;
};

const DEM_EXT = '.dem';

/**
 * A drop zone + file picker for a single CS2 .dem file. Accepts drag-and-drop or
 * click-to-browse and validates the extension, then hands the File to the
 * parent. No bytes are read here — parsing is mocked downstream.
 */
export function DemoDropzone({ onFile }: DemoDropzoneProps) {
  const inputRef = useRef<HTMLInputElement>(null);
  const [dragging, setDragging] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const accept = useCallback(
    (file: File | undefined) => {
      if (!file) return;
      if (!file.name.toLowerCase().endsWith(DEM_EXT)) {
        setError('That file is not a .dem demo.');
        return;
      }
      setError(null);
      onFile(file);
    },
    [onFile],
  );

  return (
    <div className="flex flex-col gap-3">
      <button
        type="button"
        onClick={() => inputRef.current?.click()}
        onDragOver={(e) => {
          e.preventDefault();
          setDragging(true);
        }}
        onDragLeave={() => setDragging(false)}
        onDrop={(e) => {
          e.preventDefault();
          setDragging(false);
          accept(e.dataTransfer.files?.[0]);
        }}
        className={cn(
          'flex flex-col items-center justify-center gap-3 rounded-xl border-2 border-dashed px-6 py-14 text-center transition-colors',
          dragging
            ? 'border-primary bg-primary/5'
            : 'border-border bg-card/50 hover:border-muted-foreground/40',
        )}
      >
        <span className="inline-flex size-12 items-center justify-center rounded-full border border-border bg-muted text-muted-foreground">
          <UploadCloud className="size-6" />
        </span>
        <span className="font-[family-name:var(--font-display)] text-lg font-semibold tracking-tight text-foreground">
          Drop a .dem here
        </span>
        <span className="text-sm text-muted-foreground">
          or <span className="text-primary">browse</span> your files — yours or
          anyone&apos;s
        </span>
      </button>

      <input
        ref={inputRef}
        type="file"
        accept=".dem"
        className="hidden"
        onChange={(e) => accept(e.target.files?.[0])}
      />

      {error ? (
        <p className="text-sm text-destructive">{error}</p>
      ) : (
        <p className="font-[family-name:var(--font-mono)] text-xs uppercase tracking-wider text-muted-foreground">
          No login needed · the .dem never leaves your machine
        </p>
      )}
    </div>
  );
}
