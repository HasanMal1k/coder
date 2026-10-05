"use client";

import { ClockIcon, CodeIcon, HardDriveIcon, TriangleAlertIcon } from "lucide-react";
import { useId, useState, type FormEvent } from "react";
import { errorMessage, type PowerName, type StackName } from "@/lib/api";
import {
  formatDuration,
  INSTANCE_NAME_MAX,
  nameProblem,
  suggestName,
  tidyName,
} from "@/lib/format";
import { useInstances } from "@/lib/instances";
import {
  DEFAULT_DISK_GB,
  DEFAULT_POWER,
  DEFAULT_STACK,
  DISKS_GB,
  POWERS,
  STACKS,
} from "@/lib/sizes";
import { cn } from "@/lib/utils";
import { useShell } from "../shell/shell-provider";
import { Alert, AlertDescription } from "../ui/alert";
import { Button } from "../ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "../ui/dialog";
import { Field, FieldDescription, FieldError, FieldLabel } from "../ui/field";
import { Input } from "../ui/input";
import { Spinner } from "../ui/spinner";

export function CreateMachineDialog({ open, onClose }: { open: boolean; onClose: () => void }) {
  return (
    <Dialog open={open} onOpenChange={(next) => !next && onClose()}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Create a machine</DialogTitle>
          <DialogDescription>Your own Linux computer, ready in a few seconds.</DialogDescription>
        </DialogHeader>
        {/* Rendered only while open, so the form starts fresh each time. */}
        {open && <CreateMachineForm onClose={onClose} />}
      </DialogContent>
    </Dialog>
  );
}

function CreateMachineForm({ onClose }: { onClose: () => void }) {
  const { instances, create } = useInstances();
  const { idleStopSeconds } = useShell();
  const taken = (instances ?? []).map((i) => i.name);

  // Start from a ready-made name, so the quickest path is just pressing Enter.
  const [name, setName] = useState(() => suggestName(taken));
  const [power, setPower] = useState<PowerName>(DEFAULT_POWER);
  const [diskGb, setDiskGb] = useState(DEFAULT_DISK_GB);
  const [stack, setStack] = useState<StackName>(DEFAULT_STACK);
  const [touched, setTouched] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const problem =
    nameProblem(name) ??
    (taken.includes(name) ? "You already have a machine with that name." : null);
  const shownProblem = touched ? problem : null;

  async function onSubmit(event: FormEvent) {
    event.preventDefault();
    setTouched(true);
    if (problem) return;

    setError(null);
    setSubmitting(true);
    try {
      await create(name, power, diskGb, stack);
      onClose();
    } catch (err) {
      setError(errorMessage(err));
      setSubmitting(false);
    }
  }

  const included = [
    { icon: HardDriveIcon, text: "Your home folder is kept when the machine stops" },
    { icon: CodeIcon, text: "Ubuntu 24.04 with VS Code, a terminal, sudo, Python and Node.js" },
    ...(idleStopSeconds
      ? [
          {
            icon: ClockIcon,
            text: `Stops by itself ${formatDuration(idleStopSeconds)} after you close coder, and starts again in a few seconds`,
          },
        ]
      : []),
  ];

  return (
    <form onSubmit={onSubmit} noValidate className="flex flex-col gap-5">
      {error && (
        <Alert variant="destructive">
          <TriangleAlertIcon />
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      )}

      <Field data-invalid={shownProblem ? true : undefined}>
        <FieldLabel htmlFor="machine-name">Name</FieldLabel>
        <Input
          id="machine-name"
          autoFocus
          autoComplete="off"
          spellCheck={false}
          maxLength={INSTANCE_NAME_MAX}
          className="font-mono"
          value={name}
          aria-invalid={shownProblem ? true : undefined}
          onFocus={(e) => e.target.select()}
          onChange={(e) => {
            setName(tidyName(e.target.value));
            setTouched(true);
            setError(null);
          }}
        />
        {shownProblem ? (
          <FieldError>{shownProblem}</FieldError>
        ) : (
          <FieldDescription>
            Lowercase letters, digits and hyphens. It becomes the machine&apos;s hostname.
          </FieldDescription>
        )}
      </Field>

      <Choice
        label="Power"
        hint={POWERS.find((p) => p.id === power)?.for}
        value={power}
        onChange={setPower}
        options={POWERS.map((p) => ({
          value: p.id,
          label: p.label,
          detail: `${p.cpus} vCPU · ${p.memoryGb} GB`,
        }))}
      />

      <Choice
        label="Comes with"
        hint={STACKS.find((s) => s.id === stack)?.has}
        value={stack}
        onChange={setStack}
        options={STACKS.map((s) => ({ value: s.id, label: s.label }))}
      />

      <Choice
        label="Storage"
        value={diskGb}
        onChange={setDiskGb}
        options={DISKS_GB.map((gb) => ({ value: gb, label: `${gb} GB` }))}
      />

      <ul className="flex flex-col gap-2.5 rounded-lg bg-muted/60 p-4 text-sm text-muted-foreground">
        {included.map((item) => (
          <li key={item.text} className="flex items-start gap-2.5">
            <item.icon className="mt-0.5 size-4 shrink-0 text-primary dark:text-foreground" />
            {item.text}
          </li>
        ))}
      </ul>

      <DialogFooter>
        <Button type="button" variant="outline" onClick={onClose}>
          Cancel
        </Button>
        <Button type="submit" disabled={submitting}>
          {submitting && <Spinner />}
          Create machine
        </Button>
      </DialogFooter>
    </form>
  );
}

/** A small set of options, one of which is chosen. */
function Choice<T extends string | number>({
  label,
  hint,
  value,
  onChange,
  options,
}: {
  label: string;
  /** A few words about the option that is chosen. */
  hint?: string;
  value: T;
  onChange: (value: T) => void;
  options: { value: T; label: string; detail?: string }[];
}) {
  const id = useId();

  return (
    <div className="flex flex-col gap-2">
      <div className="flex items-baseline justify-between gap-3">
        <span id={id} className="text-sm font-medium">
          {label}
        </span>
        {hint && <span className="truncate text-xs text-muted-foreground">{hint}</span>}
      </div>
      <div role="radiogroup" aria-labelledby={id} className="flex gap-2">
        {options.map((option) => {
          const chosen = option.value === value;

          return (
            <button
              key={option.value}
              type="button"
              role="radio"
              aria-checked={chosen}
              onClick={() => onChange(option.value)}
              className={cn(
                // Equal widths, except where a label needs more than its share.
                "flex-1 rounded-md border px-3 py-2 text-left text-sm whitespace-nowrap transition-colors outline-none focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/50",
                chosen
                  ? "border-foreground bg-accent text-foreground"
                  : "bg-input/30 text-muted-foreground hover:border-foreground/30 hover:text-foreground",
              )}
            >
              <span className="block font-medium">{option.label}</span>
              {option.detail && <span className="block text-xs opacity-80">{option.detail}</span>}
            </button>
          );
        })}
      </div>
    </div>
  );
}
