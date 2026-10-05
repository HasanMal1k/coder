"use client";

import { PanelRightIcon } from "lucide-react";
import { useRouter } from "next/navigation";
import { useRef, useState, type KeyboardEvent, type PointerEvent } from "react";
import type { Chat } from "@/lib/api";
import { useTitle } from "@/lib/use-title";
import { cn } from "@/lib/utils";
import { SidePane } from "../pane/side-pane";
import { PageHeader } from "../shell/page-header";
import { Button } from "../ui/button";
import {
  CHAT_DEFAULT_WIDTH,
  CHAT_MAX_WIDTH,
  CHAT_MIN_WIDTH,
  chatHref,
  useAssistant,
} from "./assistant-provider";
import { Conversation, NewConversation } from "./conversation";

/** How far an arrow key moves the divider, in pixels. */
const KEY_STEP = 24;

/**
 * The app's main page: a chat, and beside it a panel showing what that chat
 * is building. Without a chat ID it is the start of a new one.
 */
export function ChatPage({ chatId }: { chatId?: string }) {
  const router = useRouter();
  const { chats, beside, designOpen, setDesignOpen, chatWidth } = useAssistant();

  // The chat as its conversation reports it, which is ahead of the list.
  const [chat, setChat] = useState<Chat | null>(null);
  const shown = (chat?.id === chatId ? chat : null) ?? chats?.find((c) => c.id === chatId);
  const title = chatId ? shown?.title : "New chat";

  useTitle(title ?? "Chat");

  // With the panel beside it, the chat is a column of a width the user
  // chooses. Otherwise it has the page.
  const split = beside && designOpen;

  return (
    <div className="flex min-h-0 flex-1">
      {/* A container of its own, so the chat lays itself out by the room it
          actually has and not by the page's. */}
      <div
        style={split ? { width: chatWidth } : undefined}
        className={cn(
          "@container/page relative flex min-w-0 flex-col",
          split ? "max-w-[60%] shrink-0" : "flex-1",
        )}
      >
        <PageHeader
          actions={
            <Button
              variant={designOpen ? "secondary" : "ghost"}
              size="sm"
              aria-label="Panel"
              aria-pressed={designOpen}
              onClick={() => setDesignOpen(!designOpen)}
            >
              <PanelRightIcon />
              <span className="hidden @2xl/page:inline">Panel</span>
            </Button>
          }
        >
          <h1 className="truncate text-sm font-medium">{title}</h1>
        </PageHeader>

        {chatId ? (
          // Keyed by chat, so opening another one starts from a clean slate.
          <Conversation key={chatId} id={chatId} compact={split} onLoaded={setChat} />
        ) : (
          <NewConversation
            compact={split}
            onStarted={(started) => router.push(chatHref(started.id))}
          />
        )}

        {split && <Divider />}
      </div>

      <SidePane chatId={chatId} />
    </div>
  );
}

/**
 * The line between the chat and the panel, which can be dragged, or moved
 * with the arrow keys when it has focus, to give one more room and the other
 * less.
 */
function Divider() {
  const { chatWidth, setChatWidth } = useAssistant();
  const dragging = useRef(false);

  function onPointerDown(event: PointerEvent<HTMLDivElement>) {
    if (event.button !== 0) return;
    dragging.current = true;
    // From here every pointer event comes to the divider, wherever the
    // pointer goes. Without this the diagram would take them the moment the
    // pointer crossed into it.
    event.currentTarget.setPointerCapture(event.pointerId);
    event.preventDefault();
  }

  function onPointerMove(event: PointerEvent<HTMLDivElement>) {
    if (!dragging.current) return;
    const column = event.currentTarget.parentElement;
    if (column) setChatWidth(event.clientX - column.getBoundingClientRect().left);
  }

  function onPointerUp(event: PointerEvent<HTMLDivElement>) {
    dragging.current = false;
    event.currentTarget.releasePointerCapture(event.pointerId);
  }

  function onKeyDown(event: KeyboardEvent<HTMLDivElement>) {
    if (event.key === "ArrowRight") setChatWidth(chatWidth + KEY_STEP);
    else if (event.key === "ArrowLeft") setChatWidth(chatWidth - KEY_STEP);
    else if (event.key === "Home") setChatWidth(CHAT_MIN_WIDTH);
    else if (event.key === "End") setChatWidth(CHAT_MAX_WIDTH);
    else return;
    event.preventDefault();
  }

  return (
    <div
      role="separator"
      aria-orientation="vertical"
      aria-label="Resize the chat and the panel"
      aria-valuemin={CHAT_MIN_WIDTH}
      aria-valuemax={CHAT_MAX_WIDTH}
      aria-valuenow={chatWidth}
      tabIndex={0}
      onPointerDown={onPointerDown}
      onPointerMove={onPointerMove}
      onPointerUp={onPointerUp}
      onPointerCancel={onPointerUp}
      onDoubleClick={() => setChatWidth(CHAT_DEFAULT_WIDTH)}
      onKeyDown={onKeyDown}
      className="absolute inset-y-0 -right-1 z-10 w-2 cursor-col-resize touch-none outline-none after:absolute after:inset-y-0 after:left-1/2 after:w-0.5 after:-translate-x-1/2 after:bg-transparent after:transition-colors hover:after:bg-primary/50 focus-visible:after:bg-primary active:after:bg-primary"
    />
  );
}
