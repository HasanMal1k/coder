"use client";

import { useState, type FormEvent } from "react";
import { errorMessage } from "@/lib/api";
import { INSTANCE_NAME_PATTERN } from "@/lib/format";
import { Button, Spinner } from "./ui";

export function CreateInstanceForm({ onCreate }: { onCreate: (name: string) => Promise<void> }) {
  const [name, setName] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function onSubmit(event: FormEvent) {
    event.preventDefault();

    const trimmed = name.trim();
    if (!INSTANCE_NAME_PATTERN.test(trimmed)) {
      setError(
        "Use 1–40 lowercase letters, digits or hyphens, starting and ending with a letter or digit.",
      );
      return;
    }

    setError(null);
    setSubmitting(true);
    try {
      await onCreate(trimmed);
      setName("");
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <form onSubmit={onSubmit} className="flex flex-col gap-2">
      <div className="flex gap-2">
        <input
          aria-label="Machine name"
          aria-invalid={error !== null}
          placeholder="my-project"
          autoComplete="off"
          spellCheck={false}
          maxLength={40}
          value={name}
          onChange={(e) => {
            setName(e.target.value.toLowerCase());
            setError(null);
          }}
          className="h-9 min-w-0 flex-1 rounded-md border border-line-strong bg-bg px-3 font-mono text-sm outline-none placeholder:text-muted/60 focus:border-accent focus:ring-1 focus:ring-accent sm:w-64 sm:flex-none"
        />
        <Button type="submit" variant="primary" disabled={submitting || name.trim() === ""}>
          {submitting && <Spinner />}
          New machine
        </Button>
      </div>
      {error && (
        <p role="alert" className="text-sm text-danger">
          {error}
        </p>
      )}
    </form>
  );
}
