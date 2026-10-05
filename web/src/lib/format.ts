import type { Instance } from "./api";

/** Mirrors the server's rule, so most mistakes are caught before a request. */
export const INSTANCE_NAME_PATTERN = /^[a-z0-9]([a-z0-9-]{0,38}[a-z0-9])?$/;
export const INSTANCE_NAME_MAX = 40;

/** Turns what someone types into something close to a valid name. */
export function tidyName(input: string) {
  return input
    .toLowerCase()
    .replace(/[\s_.]+/g, "-")
    .replace(/[^a-z0-9-]/g, "")
    .slice(0, INSTANCE_NAME_MAX);
}

/** Why a name isn't acceptable, or null if it is. */
export function nameProblem(name: string): string | null {
  if (name === "") return "Give the machine a name.";
  if (name.startsWith("-") || name.endsWith("-")) {
    return "A name must start and end with a letter or digit.";
  }
  if (!INSTANCE_NAME_PATTERN.test(name)) {
    return "Use lowercase letters, digits and hyphens only.";
  }
  return null;
}

const ADJECTIVES = [
  "swift",
  "quiet",
  "bright",
  "calm",
  "bold",
  "keen",
  "amber",
  "lunar",
  "rapid",
  "tidy",
];
const NOUNS = [
  "otter",
  "falcon",
  "maple",
  "harbor",
  "comet",
  "willow",
  "badger",
  "summit",
  "ember",
  "delta",
];

/** A friendly default such as "swift-otter" that isn't in `taken`. */
export function suggestName(taken: string[]) {
  for (let attempt = 0; attempt < 30; attempt++) {
    const name = `${pick(ADJECTIVES)}-${pick(NOUNS)}`;
    if (!taken.includes(name)) return name;
  }
  return `machine-${Date.now().toString(36)}`;
}

function pick(list: string[]) {
  return list[Math.floor(Math.random() * list.length)];
}

export function formatMemory(mb: number) {
  return mb >= 1024 ? `${+(mb / 1024).toFixed(1)} GB` : `${mb} MB`;
}

export function formatSpecs(instance: Instance) {
  return `${instance.cpus} vCPU · ${formatMemory(instance.memory_mb)} · ${instance.disk_gb} GB disk`;
}

/** What a machine came with, if more than every machine has: "Go", ".NET". */
export function formatStack(instance: Instance) {
  const names: Record<string, string> = { go: "Go", cpp: "C++", dotnet: ".NET", data: "Data" };
  return names[instance.stack] ?? null;
}

const UNITS: [Intl.RelativeTimeFormatUnit, number][] = [
  ["year", 365 * 24 * 3600],
  ["month", 30 * 24 * 3600],
  ["day", 24 * 3600],
  ["hour", 3600],
  ["minute", 60],
];

/** "3 hours ago", "yesterday", "just now". */
export function formatRelative(iso: string, now = Date.now()) {
  const seconds = Math.round((new Date(iso).getTime() - now) / 1000);
  const formatter = new Intl.RelativeTimeFormat("en", { numeric: "auto" });

  for (const [unit, size] of UNITS) {
    if (Math.abs(seconds) >= size) {
      return formatter.format(Math.round(seconds / size), unit);
    }
  }
  return "just now";
}

/** A length of time in words: "2 minutes", "90 seconds", "1 hour". */
export function formatDuration(seconds: number) {
  const plural = (n: number, unit: string) => `${n} ${unit}${n === 1 ? "" : "s"}`;

  if (seconds % 3600 === 0) return plural(seconds / 3600, "hour");
  if (seconds % 60 === 0) return plural(seconds / 60, "minute");
  return plural(seconds, "second");
}

/** How long something has been going: "45s", "3m 05s", "1h 02m". */
export function formatElapsed(since: string, now = Date.now()) {
  const total = Math.max(0, Math.floor((now - new Date(since).getTime()) / 1000));
  const hours = Math.floor(total / 3600);
  const minutes = Math.floor((total % 3600) / 60);
  const seconds = total % 60;
  const pad = (n: number) => String(n).padStart(2, "0");

  if (hours > 0) return `${hours}h ${pad(minutes)}m`;
  if (minutes > 0) return `${minutes}m ${pad(seconds)}s`;
  return `${seconds}s`;
}

/** "1 Oct 2026, 13:14". */
export function formatDateTime(iso: string) {
  return new Intl.DateTimeFormat("en-GB", { dateStyle: "medium", timeStyle: "short" }).format(
    new Date(iso),
  );
}

/** "Ada Lovelace" → "AL"; falls back to the first letter of the email. */
export function initials(name: string, email: string) {
  const words = name.trim().split(/\s+/).filter(Boolean);
  if (words.length === 0) return email.slice(0, 1).toUpperCase();
  return (words[0][0] + (words.length > 1 ? words[words.length - 1][0] : "")).toUpperCase();
}
