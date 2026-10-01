import type { Instance } from "./api";

/** Mirrors the server's rule, so most mistakes are caught before a request. */
export const INSTANCE_NAME_PATTERN = /^[a-z0-9]([a-z0-9-]{0,38}[a-z0-9])?$/;

export function formatSpecs(instance: Instance) {
  const memory =
    instance.memory_mb >= 1024
      ? `${+(instance.memory_mb / 1024).toFixed(1)} GB`
      : `${instance.memory_mb} MB`;

  return `${instance.cpus} CPU · ${memory} RAM · ${instance.disk_gb} GB disk`;
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
