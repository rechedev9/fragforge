'use client';

import { useCallback, useId, useRef, useState } from 'react';
import { UploadCloud } from 'lucide-react';
import { cn } from '@/lib/utils';

export type DemoDropzoneProps = {
  /** Called with the chosen .dem file. The parent owns parsing + navigation. */
  onFile: (file: File) => void;
};

const DEM_EXT = '.dem';

/**
 * A drop zone + file picker for a single CS2 .dem file. The clickable area is a
 * <label> bound to the file input, so the OS file dialog opens natively on
 * click (no JS .click() that can be flaky with hidden inputs). Drag-and-drop and
 * keyboard both work; the extension is validated before handing the File up.
 */
export function DemoDropzone({ onFile }: DemoDropzoneProps) {
  const inputRef = useRef<HTMLInputElement>(null);
  const inputId = useId();
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
      <label
        htmlFor={inputId}
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
          'flex cursor-pointer flex-col items-center justify-center gap-3 rounded-xl border-2 border-dashed px-6 py-14 text-center transition-colors',
          'focus-within:border-primary focus-within:ring-2 focus-within:ring-ring/40',
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
          or <span className="text-primary underline-offset-2 group-hover:underline">browse</span> your files — yours or
          anyone&apos;s
        </span>

        <input
          id={inputId}
          ref={inputRef}
          type="file"
          // No `accept` filter: on Windows the ".dem" filter hid every file in
          // folders without a .dem, so the dialog looked empty/broken. Show all
          // files; the extension check below rejects non-.dem with a message.
          className="sr-only"
          // Reset so picking the same file again still fires onChange.
          onClick={(e) => {
            (e.target as HTMLInputElement).value = '';
          }}
          onChange={(e) => accept(e.target.files?.[0])}
        />
      </label>

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
