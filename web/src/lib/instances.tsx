"use client";

import {
  createContext,
  useCallback,
  useContext,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";
import {
  createInstance,
  deleteInstance,
  errorMessage,
  listInstances,
  startInstance,
  stopInstance,
  type Instance,
  type InstanceStatus,
  type PowerName,
  type StackName,
} from "./api";
import { useRouter } from "next/navigation";
import { toast } from "sonner";
import { usePolling } from "./use-polling";

/** An operation this app has asked for and is waiting on. */
export type PendingAction = "starting" | "stopping" | "deleting";

const REFRESH_INTERVAL_MS = 5000;

const BUSY: InstanceStatus[] = ["creating", "starting", "stopping", "deleting"];

type InstancesState = {
  /** null until the first load finishes. */
  instances: Instance[] | null;
  /** Why the list couldn't be loaded, when it couldn't. */
  loadError: string | null;
  pending: Record<string, PendingAction>;
  refresh: () => Promise<void>;
  /** Creates a machine and starts it. Rejects if it can't be created. */
  create: (name: string, size?: PowerName, diskGb?: number, stack?: StackName) => Promise<Instance>;
  start: (id: string) => Promise<void>;
  stop: (id: string) => Promise<void>;
  /** Resolves to whether the machine was deleted. */
  remove: (id: string) => Promise<boolean>;
};

const InstancesContext = createContext<InstancesState | null>(null);

function without<T>(record: Record<string, T>, key: string) {
  const copy = { ...record };
  delete copy[key];
  return copy;
}

/**
 * Holds the user's machines for every page: the list, the sidebar, the
 * machine page and the command palette all read the same state, so an action
 * taken in one shows up in the others at once.
 */
export function InstancesProvider({ children }: { children: ReactNode }) {
  const router = useRouter();

  const [instances, setInstances] = useState<Instance[] | null>(null);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [pending, setPending] = useState<Record<string, PendingAction>>({});

  // Bumped whenever one of our own changes starts or finishes. A list that
  // was requested before a change finished may not include it, so a refresh
  // that straddles a bump is thrown away instead of overwriting newer state.
  const epoch = useRef(0);

  // The latest list, for callbacks that need a machine's name without being
  // rebuilt every time the list changes.
  const latest = useRef<Instance[] | null>(null);

  const refresh = useCallback(async () => {
    const startedAt = epoch.current;
    try {
      const list = await listInstances();
      if (epoch.current !== startedAt) return;
      latest.current = list;
      setInstances(list);
      setLoadError(null);
    } catch (err) {
      if (epoch.current === startedAt) setLoadError(errorMessage(err));
    }
  }, []);

  // Keeps every page honest about changes made elsewhere (another tab, or a
  // machine that failed on its own).
  usePolling(refresh, REFRESH_INTERVAL_MS);

  const apply = useCallback((update: (list: Instance[]) => Instance[]) => {
    setInstances((list) => {
      const next = list ? update(list) : list;
      latest.current = next;
      return next;
    });
  }, []);

  /** Runs one operation on a machine, tracking it as pending meanwhile. */
  const act = useCallback(
    async (
      id: string,
      action: PendingAction,
      call: () => Promise<Instance | void>,
      messages: {
        done?: (name: string) => string;
        failed: (name: string) => string;
        open?: boolean;
      },
    ) => {
      const name = latest.current?.find((i) => i.id === id)?.name ?? "machine";

      epoch.current++;
      setPending((p) => ({ ...p, [id]: action }));

      let ok = false;
      try {
        const updated = await call();
        apply((list) =>
          updated ? list.map((i) => (i.id === id ? updated : i)) : list.filter((i) => i.id !== id),
        );
        ok = true;
        if (messages.done) {
          toast.success(messages.done(name), {
            action: messages.open
              ? { label: "Open", onClick: () => router.push(`/instances/${id}`) }
              : undefined,
          });
        }
      } catch (err) {
        toast.error(messages.failed(name), { description: errorMessage(err) });
      } finally {
        epoch.current++;
        setPending((p) => without(p, id));
        refresh();
      }

      return ok;
    },
    [apply, refresh, router],
  );

  const start = useCallback(
    async (id: string) => {
      await act(id, "starting", () => startInstance(id), {
        failed: (name) => `Couldn't start ${name}`,
      });
    },
    [act],
  );

  const stop = useCallback(
    async (id: string) => {
      await act(id, "stopping", () => stopInstance(id), {
        done: (name) => `${name} stopped. Its files are kept.`,
        failed: (name) => `Couldn't stop ${name}`,
      });
    },
    [act],
  );

  const remove = useCallback(
    (id: string) =>
      act(id, "deleting", () => deleteInstance(id), {
        done: (name) => `${name} was deleted.`,
        failed: (name) => `Couldn't delete ${name}`,
      }),
    [act],
  );

  const create = useCallback(
    async (name: string, size?: PowerName, diskGb?: number, stack?: StackName) => {
      epoch.current++;
      let created: Instance;
      try {
        created = await createInstance(name, size, diskGb, stack);
      } finally {
        epoch.current++;
      }

      apply((list) => [...list, created]);

      // A new machine is only useful running, so start it straight away. The
      // caller doesn't wait for this; the list shows the progress.
      act(created.id, "starting", () => startInstance(created.id), {
        done: (n) => `${n} is ready.`,
        failed: (n) => `${n} was created but couldn't be started`,
        open: true,
      });

      return created;
    },
    [act, apply],
  );

  const value = useMemo(
    () => ({ instances, loadError, pending, refresh, create, start, stop, remove }),
    [instances, loadError, pending, refresh, create, start, stop, remove],
  );

  return <InstancesContext.Provider value={value}>{children}</InstancesContext.Provider>;
}

export function useInstances() {
  const state = useContext(InstancesContext);
  if (!state) throw new Error("useInstances must be used inside InstancesProvider");
  return state;
}

/** The status to show for a machine: where our own request is taking it, if we have one. */
export function displayStatus(instance: Instance, pending?: PendingAction): InstanceStatus {
  return pending ?? instance.status;
}

export function isBusy(status: InstanceStatus) {
  return BUSY.includes(status);
}
