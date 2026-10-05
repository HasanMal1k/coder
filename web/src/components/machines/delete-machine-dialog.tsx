"use client";

import { useState, type FormEvent } from "react";
import type { Instance } from "@/lib/api";
import { useInstances } from "@/lib/instances";
import { Button } from "../ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "../ui/dialog";
import { Field, FieldLabel } from "../ui/field";
import { Input } from "../ui/input";

export function DeleteMachineDialog({
  instance,
  onDeleted,
  onClose,
}: {
  /** The machine to delete; null keeps the dialog closed. */
  instance: Instance | null;
  onDeleted?: () => void;
  onClose: () => void;
}) {
  return (
    <Dialog open={instance !== null} onOpenChange={(next) => !next && onClose()}>
      <DialogContent className="sm:max-w-md">
        {instance && (
          <DeleteMachineForm instance={instance} onDeleted={onDeleted} onClose={onClose} />
        )}
      </DialogContent>
    </Dialog>
  );
}

function DeleteMachineForm({
  instance,
  onDeleted,
  onClose,
}: {
  instance: Instance;
  onDeleted?: () => void;
  onClose: () => void;
}) {
  const { remove } = useInstances();
  const [typed, setTyped] = useState("");

  const confirmed = typed.trim() === instance.name;

  async function onSubmit(event: FormEvent) {
    event.preventDefault();
    if (!confirmed) return;

    // The list shows the progress, so the dialog needn't stay open for it.
    onClose();
    if (await remove(instance.id)) onDeleted?.();
  }

  return (
    <form onSubmit={onSubmit} className="flex flex-col gap-5">
      <DialogHeader>
        <DialogTitle>Delete machine</DialogTitle>
        <DialogDescription>
          This permanently deletes{" "}
          <strong className="font-mono font-semibold text-foreground">{instance.name}</strong> and
          every file on it. It can&apos;t be undone.
        </DialogDescription>
      </DialogHeader>

      <Field>
        <FieldLabel htmlFor="confirm-name">
          Type &ldquo;{instance.name}&rdquo; to confirm
        </FieldLabel>
        <Input
          id="confirm-name"
          autoFocus
          autoComplete="off"
          spellCheck={false}
          className="font-mono"
          value={typed}
          onChange={(e) => setTyped(e.target.value)}
        />
      </Field>

      <DialogFooter>
        <Button type="button" variant="outline" onClick={onClose}>
          Cancel
        </Button>
        <Button type="submit" variant="destructive" disabled={!confirmed}>
          Delete machine
        </Button>
      </DialogFooter>
    </form>
  );
}
