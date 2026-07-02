'use client';

import { useState } from 'react';
import { Trash2 } from 'lucide-react';
import { toast } from 'sonner';
import type { Video } from '@/lib/api/types';
import { api } from '@/lib/api';
import { Button } from '@/components/ui/button';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';

/**
 * Trash button with a confirm dialog: removes the reel from the library and
 * deletes its rendered files from disk (best-effort when the orchestrator is
 * offline). Deletion is not undoable, hence the explicit confirmation.
 */
export function DeleteVideoButton({ video, onDeleted }: { video: Video; onDeleted: () => void }) {
  const [open, setOpen] = useState(false);
  const [deleting, setDeleting] = useState(false);

  async function onConfirm() {
    if (deleting) return;
    setDeleting(true);
    try {
      await api.deleteVideo(video.id);
      setOpen(false);
      toast('Reel deleted.');
      onDeleted();
    } catch {
      toast('Could not delete the reel.');
    } finally {
      setDeleting(false);
    }
  }

  return (
    <>
      <Button
        variant="ghost"
        size="sm"
        aria-label={`Delete ${video.title}`}
        className="text-muted-foreground hover:text-destructive"
        onClick={() => setOpen(true)}
      >
        <Trash2 className="size-4" />
      </Button>

      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent className="max-w-sm">
          <DialogHeader>
            <DialogTitle>Delete this reel?</DialogTitle>
            <DialogDescription className="truncate">
              &ldquo;{video.title}&rdquo; and its rendered file will be removed. This cannot be
              undone.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" size="sm" onClick={() => setOpen(false)} disabled={deleting}>
              Cancel
            </Button>
            <Button variant="destructive" size="sm" onClick={onConfirm} disabled={deleting}>
              <Trash2 className="size-4" />
              {deleting ? 'Deleting…' : 'Delete'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}
