"use client";

import { useCallback, useRef, useState } from "react";
import {
  createInstance,
  deleteInstance,
  errorMessage,
  listInstances,
  startInstance,
  stopInstance,
  type Instance,
} from "@/lib/api";
import type { Session } from "@/lib/session";
import { usePolling } from "@/lib/use-polling";
import { AppHeader } from "./app-header";
import { CreateInstanceForm } from "./create-instance-form";
import { InstanceCard, type PendingAction } from "./instance-card";
import { ErrorNote, PageSpinner } from "./ui";

const REFRESH_INTERVAL_MS = 5000;

function without<T>(record: Record<string, T>, key: string) {
  const copy = { ...record };
  delete copy[key];
  return copy;
}

export function Dashboard({ session }: { session: Session }) {
  const { token } = session;

  const [instances, setInstances] = useState<Instance[] | null>(null);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [pending, setPending] = useState<Record<string, PendingAction>>({});
  const [errors, setErrors] = useState<Record<string, string>>({});

  // Bumped whenever one of our own changes starts or finishes. A list that
  // was requested before a change finished may not include it, so a refresh
  // that straddles a bump is thrown away instead of overwriting newer state.
  const epoch = useRef(0);

  const refresh = useCallback(async () => {
    const startedAt = epoch.current;
    try {
      const list = await listInstances(token);
      if (epoch.current !== startedAt) return;
      setInstances(list);
      setLoadError(null);
    } catch (err) {
      if (epoch.current === startedAt) setLoadError(errorMessage(err));
    }
  }, [token]);

  // Keeps the page honest about changes made elsewhere (another tab, or a
  // machine that failed on its own).
  usePolling(refresh, REFRESH_INTERVAL_MS);

  /** Runs one operation on an instance, tracking it as pending meanwhile. */
  async function act(id: string, action: PendingAction, call: () => Promise<Instance | void>) {
    epoch.current++;
    setPending((p) => ({ ...p, [id]: action }));
    setErrors((e) => without(e, id));

    try {
      const updated = await call();
      setInstances(
        (list) =>
          list &&
          (updated ? list.map((i) => (i.id === id ? updated : i)) : list.filter((i) => i.id !== id)),
      );
    } catch (err) {
      setErrors((e) => ({ ...e, [id]: errorMessage(err) }));
    } finally {
      epoch.current++;
      setPending((p) => without(p, id));
      refresh();
    }
  }

  async function create(name: string) {
    epoch.current++;
    let created: Instance;
    try {
      created = await createInstance(token, name);
    } finally {
      epoch.current++;
    }

    setInstances((list) => [...(list ?? []), created]);

    // A new machine is only useful running, so start it straight away. The
    // form doesn't wait for this; the card shows the progress.
    act(created.id, "starting", () => startInstance(token, created.id));
  }

  return (
    <>
      <AppHeader session={session} />

      <main className="mx-auto flex w-full max-w-4xl flex-1 flex-col gap-6 px-4 py-8 sm:px-6">
        <div className="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
          <div>
            <h1 className="text-xl font-semibold">Machines</h1>
            <p className="mt-1 text-sm text-muted">
              Each one is your own Linux computer. Files in your home folder are kept when it stops.
            </p>
          </div>
          <CreateInstanceForm onCreate={create} />
        </div>

        {loadError && <ErrorNote>{loadError}</ErrorNote>}

        {instances === null ? (
          !loadError && <PageSpinner label="Loading machines" />
        ) : instances.length === 0 ? (
          <div className="rounded-xl border border-dashed border-line-strong px-6 py-16 text-center">
            <p className="font-medium">No machines yet</p>
            <p className="mt-1 text-sm text-muted">
              Give one a name above and it will be ready in a few seconds.
            </p>
          </div>
        ) : (
          <ul className="flex flex-col gap-3">
            {instances.map((instance) => (
              <InstanceCard
                key={instance.id}
                instance={instance}
                pending={pending[instance.id]}
                error={errors[instance.id]}
                onStart={() => act(instance.id, "starting", () => startInstance(token, instance.id))}
                onStop={() => act(instance.id, "stopping", () => stopInstance(token, instance.id))}
                onDelete={() => act(instance.id, "deleting", () => deleteInstance(token, instance.id))}
              />
            ))}
          </ul>
        )}
      </main>
    </>
  );
}
