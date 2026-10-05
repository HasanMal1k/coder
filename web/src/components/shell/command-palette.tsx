"use client";

import {
  LogOutIcon,
  MessageSquareIcon,
  MonitorIcon,
  PlayIcon,
  PlusIcon,
  SquareIcon,
  SquarePenIcon,
  UserIcon,
} from "lucide-react";
import { useRouter } from "next/navigation";
import { displayStatus, isBusy, useInstances } from "@/lib/instances";
import { clearSession } from "@/lib/session";
import { chatHref, useAssistant } from "../assistant/assistant-provider";
import { StatusDot } from "../status";
import {
  Command,
  CommandDialog,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
  CommandSeparator,
} from "../ui/command";
import { useShell } from "./shell-provider";

/** Search across chats, machines and actions, from anywhere in the app. */
export function CommandPalette({ open, onClose }: { open: boolean; onClose: () => void }) {
  const router = useRouter();
  const { instances, pending, start, stop } = useInstances();
  const { openCreate } = useShell();
  const { chats } = useAssistant();

  /** Closes the palette, then does the thing. */
  const run = (action: () => void) => () => {
    onClose();
    action();
  };

  const machines = (instances ?? []).map((instance) => ({
    instance,
    status: displayStatus(instance, pending[instance.id]),
  }));
  const startable = machines.filter((m) => m.status === "stopped" || m.status === "error");
  const stoppable = machines.filter((m) => m.status === "running" && !isBusy(m.status));

  return (
    <CommandDialog
      open={open}
      onOpenChange={(next) => !next && onClose()}
      title="Search chats, machines and commands"
      description="Jump to a chat or a machine, start or stop one, or run a command."
    >
      {/* The dialog is only a frame; the searchable list has to be a
          Command of its own inside it. */}
      <Command>
        <CommandInput placeholder="Search chats, machines and commands" />
        <CommandList>
          <CommandEmpty>Nothing matches.</CommandEmpty>

          {chats && chats.length > 0 && (
            <CommandGroup heading="Chats">
              {chats.map((chat) => (
                <CommandItem
                  key={chat.id}
                  // Titles can repeat; the ID keeps each entry its own.
                  value={`chat ${chat.title} ${chat.id}`}
                  onSelect={run(() => router.push(chatHref(chat.id)))}
                >
                  <MessageSquareIcon />
                  <span className="truncate">{chat.title}</span>
                </CommandItem>
              ))}
            </CommandGroup>
          )}

          {machines.length > 0 && (
            <CommandGroup heading="Machines">
              {machines.map(({ instance, status }) => (
                <CommandItem
                  key={instance.id}
                  value={`open ${instance.name}`}
                  onSelect={run(() => router.push(`/instances/${instance.id}`))}
                >
                  <StatusDot status={status} className="mx-1" />
                  <span className="font-mono">{instance.name}</span>
                </CommandItem>
              ))}
            </CommandGroup>
          )}

          <CommandGroup heading="Actions">
            <CommandItem
              value="new chat start build ask assistant"
              onSelect={run(() => router.push(chatHref(null)))}
            >
              <SquarePenIcon />
              New chat
            </CommandItem>
            <CommandItem value="create a machine new add" onSelect={run(openCreate)}>
              <PlusIcon />
              Create a machine
            </CommandItem>
            {startable.map(({ instance }) => (
              <CommandItem
                key={instance.id}
                value={`start ${instance.name}`}
                onSelect={run(() => start(instance.id))}
              >
                <PlayIcon />
                Start {instance.name}
              </CommandItem>
            ))}
            {stoppable.map(({ instance }) => (
              <CommandItem
                key={instance.id}
                value={`stop ${instance.name}`}
                onSelect={run(() => stop(instance.id))}
              >
                <SquareIcon />
                Stop {instance.name}
              </CommandItem>
            ))}
          </CommandGroup>

          <CommandSeparator />

          <CommandGroup heading="Go to">
            <CommandItem
              value="go to machines list all"
              onSelect={run(() => router.push("/machines"))}
            >
              <MonitorIcon />
              Machines
            </CommandItem>
            <CommandItem
              value="go to account profile"
              onSelect={run(() => router.push("/account"))}
            >
              <UserIcon />
              Account
            </CommandItem>
            <CommandItem value="sign out log out logout" onSelect={run(() => clearSession())}>
              <LogOutIcon />
              Sign out
            </CommandItem>
          </CommandGroup>
        </CommandList>
      </Command>
    </CommandDialog>
  );
}
