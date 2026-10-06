import type { PowerName, StackName } from "./api";

/**
 * The sizes a machine can be created in. They mirror the server's
 * (internal/instance/size.go), which is what decides; these are for showing
 * the choice.
 */
export const POWERS: {
  id: PowerName;
  label: string;
  cpus: number;
  memoryGb: number;
  for: string;
}[] = [
  { id: "small", label: "Small", cpus: 1, memoryGb: 1, for: "A small server or a script" },
  { id: "medium", label: "Medium", cpus: 2, memoryGb: 2, for: "Most projects" },
  { id: "large", label: "Large", cpus: 4, memoryGb: 4, for: "Heavy builds, several services" },
];

export const DISKS_GB = [10, 20, 50];

/**
 * The toolchains a machine can come with. Every machine has Python and
 * Node.js; a stack adds to that.
 */
export const STACKS: { id: StackName; label: string; has: string }[] = [
  { id: "base", label: "Standard", has: "Python and Node.js" },
  { id: "go", label: "Go", has: "Go toolchain" },
  { id: "cpp", label: "C++", has: "GCC, Clang, CMake, GDB" },
  { id: "dotnet", label: ".NET", has: "C# and ASP.NET Core" },
  { id: "data", label: "Data", has: "PostgreSQL and Redis" },
];

export const DEFAULT_STACK: StackName = "base";

export function stackLabel(stack?: StackName) {
  return STACKS.find((s) => s.id === stack)?.label ?? "Standard";
}

/** A size by its name alone, for where there is no room for its figures. */
export function powerName(size?: PowerName) {
  return POWERS.find((p) => p.id === size)?.label ?? "";
}

export const DEFAULT_POWER: PowerName = "medium";
export const DEFAULT_DISK_GB = 10;
