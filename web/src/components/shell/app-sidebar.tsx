"use client";

import {
  ChevronsUpDownIcon,
  LogOutIcon,
  MonitorIcon,
  MoreHorizontalIcon,
  SquarePenIcon,
  Trash2Icon,
  UserIcon,
} from "lucide-react";
import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { useMemo, useState } from "react";
import type { Chat } from "@/lib/api";
import { useAgents, useNow } from "@/lib/agents";
import { initials } from "@/lib/format";
import { clearSession } from "@/lib/session";
import { chatHref, useAssistant } from "../assistant/assistant-provider";
import { removeChat } from "../assistant/conversation";
import { DeleteChatDialog } from "../assistant/delete-chat-dialog";
import { useUser } from "../auth/require-auth";
import { BrandMark } from "../brand";
import { Avatar, AvatarFallback } from "../ui/avatar";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "../ui/dropdown-menu";
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarGroup,
  SidebarGroupContent,
  SidebarGroupLabel,
  SidebarHeader,
  SidebarMenu,
  SidebarMenuAction,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarMenuSkeleton,
  SidebarRail,
  useSidebar,
} from "../ui/sidebar";
import { Spinner } from "../ui/spinner";

const DAY_MS = 24 * 60 * 60 * 1000;

/** Chats under a heading saying how long ago each was last active. */
function groupByAge(chats: Chat[], now: number) {
  const today = new Date(now).setHours(0, 0, 0, 0);
  const groups: { label: string; chats: Chat[] }[] = [
    { label: "Today", chats: [] },
    { label: "Yesterday", chats: [] },
    { label: "Previous 7 days", chats: [] },
    { label: "Older", chats: [] },
  ];

  for (const chat of chats) {
    const at = new Date(chat.updated_at).getTime();
    const group = at >= today ? 0 : at >= today - DAY_MS ? 1 : at >= today - 7 * DAY_MS ? 2 : 3;
    groups[group].chats.push(chat);
  }

  return groups.filter((group) => group.chats.length > 0);
}

/** A way to start a chat, the chats so far, and who is signed in. */
export function AppSidebar() {
  const pathname = usePathname();
  const router = useRouter();
  const user = useUser();
  const { chats, chatId, refreshChats } = useAssistant();
  const { agents } = useAgents();
  const { isMobile, setOpenMobile } = useSidebar();
  // Only the day matters here, so a look every minute is plenty.
  const now = useNow(60000);

  const groups = useMemo(() => groupByAge(chats ?? [], now), [chats, now]);

  // A chat with an agent still at work in the background is busy too, though
  // the assistant itself has finished speaking.
  const busy = useMemo(
    () => new Set((agents ?? []).filter((a) => a.status === "working").map((a) => a.chat_id)),
    [agents],
  );

  // The chat the user has asked to delete and not yet confirmed.
  const [deleting, setDeleting] = useState<Chat | null>(null);

  const name = user.display_name || user.email;
  // On a phone the sidebar is a drawer over the page; picking something
  // should put it away.
  const closeDrawer = () => setOpenMobile(false);

  async function remove(chat: Chat) {
    if (!(await removeChat(chat.id))) return;
    refreshChats();
    if (chat.id === chatId) router.push(chatHref(null));
  }

  return (
    <Sidebar collapsible="icon">
      <SidebarHeader>
        <SidebarMenu>
          <SidebarMenuItem>
            <SidebarMenuButton size="lg" asChild tooltip="coder">
              <Link href="/" onClick={closeDrawer}>
                <BrandMark />
                <span className="text-base font-semibold tracking-tight">coder</span>
              </Link>
            </SidebarMenuButton>
          </SidebarMenuItem>
        </SidebarMenu>
      </SidebarHeader>

      <SidebarContent>
        <SidebarGroup>
          <SidebarGroupContent>
            <SidebarMenu>
              <SidebarMenuItem>
                <SidebarMenuButton asChild isActive={pathname === "/"} tooltip="New chat">
                  <Link href="/" onClick={closeDrawer}>
                    <SquarePenIcon />
                    <span>New chat</span>
                  </Link>
                </SidebarMenuButton>
              </SidebarMenuItem>
            </SidebarMenu>
          </SidebarGroupContent>
        </SidebarGroup>

        {/* Hidden when the sidebar is collapsed to icons: a chat is only its
            title, and there is no room for one. */}
        <nav aria-label="Chats" className="contents group-data-[collapsible=icon]:hidden">
          {chats === null && (
            <SidebarGroup>
              <SidebarGroupLabel>Chats</SidebarGroupLabel>
              <SidebarGroupContent>
                <SidebarMenu>
                  <SidebarMenuSkeleton />
                  <SidebarMenuSkeleton />
                  <SidebarMenuSkeleton />
                </SidebarMenu>
              </SidebarGroupContent>
            </SidebarGroup>
          )}

          {chats?.length === 0 && (
            <SidebarGroup>
              <SidebarGroupLabel>Chats</SidebarGroupLabel>
              <SidebarGroupContent>
                <p className="px-3 py-1.5 text-sm text-sidebar-foreground/60">
                  Nothing yet. Each thing you build gets a chat of its own, listed here.
                </p>
              </SidebarGroupContent>
            </SidebarGroup>
          )}

          {groups.map((group) => (
            <SidebarGroup key={group.label}>
              <SidebarGroupLabel>{group.label}</SidebarGroupLabel>
              <SidebarGroupContent>
                <SidebarMenu>
                  {group.chats.map((chat) => (
                    <ChatLink
                      key={chat.id}
                      chat={chat}
                      current={chat.id === chatId}
                      working={chat.status === "running" || busy.has(chat.id)}
                      onNavigate={closeDrawer}
                      onDelete={() => setDeleting(chat)}
                    />
                  ))}
                </SidebarMenu>
              </SidebarGroupContent>
            </SidebarGroup>
          ))}
        </nav>
      </SidebarContent>

      <SidebarFooter>
        <SidebarMenu>
          <SidebarMenuItem>
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <SidebarMenuButton
                  size="lg"
                  aria-label="Account menu"
                  className="data-[state=open]:bg-sidebar-accent data-[state=open]:text-sidebar-accent-foreground"
                >
                  <Avatar className="size-8">
                    <AvatarFallback className="border bg-muted text-xs font-semibold text-foreground">
                      {initials(user.display_name, user.email)}
                    </AvatarFallback>
                  </Avatar>
                  <div className="grid flex-1 text-left leading-tight">
                    <span className="truncate text-sm font-medium">{name}</span>
                    <span className="truncate text-xs text-sidebar-foreground/60">
                      {user.email}
                    </span>
                  </div>
                  <ChevronsUpDownIcon className="ml-auto size-4 text-sidebar-foreground/60" />
                </SidebarMenuButton>
              </DropdownMenuTrigger>
              <DropdownMenuContent
                side={isMobile ? "top" : "right"}
                align="end"
                sideOffset={8}
                className="min-w-56"
              >
                <DropdownMenuLabel className="font-normal">
                  <p className="truncate text-sm font-medium text-foreground">{name}</p>
                  <p className="truncate text-xs text-muted-foreground">{user.email}</p>
                </DropdownMenuLabel>
                <DropdownMenuSeparator />
                <DropdownMenuItem asChild>
                  <Link href="/machines" onClick={closeDrawer}>
                    <MonitorIcon />
                    Machines
                  </Link>
                </DropdownMenuItem>
                <DropdownMenuItem asChild>
                  <Link href="/account" onClick={closeDrawer}>
                    <UserIcon />
                    Account
                  </Link>
                </DropdownMenuItem>
                <DropdownMenuSeparator />
                <DropdownMenuItem onSelect={() => clearSession()}>
                  <LogOutIcon />
                  Sign out
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          </SidebarMenuItem>
        </SidebarMenu>
      </SidebarFooter>

      <SidebarRail />

      <DeleteChatDialog chat={deleting} onConfirm={remove} onClose={() => setDeleting(null)} />
    </Sidebar>
  );
}

/** One chat in the list: its title, whether it needs anything, and a menu. */
function ChatLink({
  chat,
  current,
  working,
  onNavigate,
  onDelete,
}: {
  chat: Chat;
  current: boolean;
  /** The assistant, or an agent it started, is at work on it right now. */
  working: boolean;
  onNavigate: () => void;
  onDelete: () => void;
}) {
  return (
    <SidebarMenuItem>
      <SidebarMenuButton asChild isActive={current}>
        <Link
          href={chatHref(chat.id)}
          onClick={onNavigate}
          title={chat.title}
          aria-current={current ? "page" : undefined}
        >
          <span className="min-w-0 flex-1 truncate">{chat.title}</span>
          {working ? (
            <Spinner className="size-3.5 text-primary" aria-label="Working" />
          ) : (
            chat.status === "waiting" && (
              <span
                role="img"
                aria-label="Needs an answer"
                title="Needs an answer"
                className="size-2 shrink-0 rounded-full bg-warning"
              />
            )
          )}
        </Link>
      </SidebarMenuButton>

      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <SidebarMenuAction showOnHover aria-label={`More for ${chat.title}`}>
            <MoreHorizontalIcon />
          </SidebarMenuAction>
        </DropdownMenuTrigger>
        <DropdownMenuContent side="right" align="start" className="min-w-40">
          <DropdownMenuItem variant="destructive" onSelect={onDelete}>
            <Trash2Icon />
            Delete chat…
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
    </SidebarMenuItem>
  );
}
