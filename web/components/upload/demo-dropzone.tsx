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
 * A drop zone + file picker for a single CS2 .dem file, styled as the mockup's
 * HUD dropzone: dashed cyan border, four corner brackets, glowing circle icon.
 * The clickable area is a <label> bound to the file input, so the OS file
 * dialog opens natively on click (no JS .click() that can be flaky with hidden
 * inputs). Drag-and-drop and keyboard both work; the extension is validated
 * before handing the File up.
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
        setError('Ese archivo no es una demo .dem.');
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
          'relative flex cursor-pointer flex-col items-center justify-center border-[1.5px] border-dashed px-6 py-16 text-center transition-colors',
          'focus-within:border-primary focus-within:ring-2 focus-within:ring-ring/40',
          dragging ? 'border-primary bg-primary/10' : 'border-primary/40 bg-card/55 hover:border-primary/70',
        )}
      >
        {/* The four HUD corner brackets. */}
        <span aria-hidden className="absolute -top-0.5 -left-0.5 size-[18px] border-t-[2.5px] border-l-[2.5px] border-primary" />
        <span aria-hidden className="absolute -top-0.5 -right-0.5 size-[18px] border-t-[2.5px] border-r-[2.5px] border-primary" />
        <span aria-hidden className="absolute -bottom-0.5 -left-0.5 size-[18px] border-b-[2.5px] border-l-[2.5px] border-primary" />
        <span aria-hidden className="absolute -bottom-0.5 -right-0.5 size-[18px] border-b-[2.5px] border-r-[2.5px] border-primary" />

        <span className="flex size-16 items-center justify-center rounded-full border border-primary/40 text-primary [box-shadow:0_0_24px_color-mix(in_oklch,var(--primary)_25%,transparent),inset_0_0_14px_color-mix(in_oklch,var(--primary)_15%,transparent)]">
          <UploadCloud className="size-7" />
        </span>
        <span className="mt-5 font-[family-name:var(--font-display)] text-2xl font-bold text-foreground">
          SUELTA UN .DEM AQUÍ
        </span>
        <span className="mt-2 text-sm text-muted-foreground">
          o <span className="text-primary underline decoration-primary/50 underline-offset-4">explora tus archivos</span> — tuyo o de
          cualquiera
        </span>
        <span className="mt-4 font-[family-name:var(--font-mono)] text-[10.5px] tracking-[0.2em] text-muted-foreground/70">
          SIN LOGIN · EL .DEM NUNCA SALE DE TU PC
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

      {error ? <p className="text-sm text-destructive">{error}</p> : null}
    </div>
  );
}
