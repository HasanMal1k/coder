"use client";

import { usePathname } from "next/navigation";
import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from "react";
import { assistantInfo, type Chat } from "@/lib/api";
import { useChats } from "@/lib/chat";
import { useMediaQuery } from "@/lib/use-media-query";
import { usePolling } from "@/lib/use-polling";
import { useUser } from "../auth/require-auth";

const INFO_REFRESH_MS = 60000;

/** From this width up the design sits beside the chat; below, over it. */
const BESIDE_QUERY = "(min-width: 1100px)";

export const CHAT_MIN_WIDTH = 360;
export const CHAT_MAX_WIDTH = 760;
export const CHAT_DEFAULT_WIDTH = 460;

/** What the panel beside a chat can show. */
export type PaneTab = "graph" | "frontend" | "backend" | "dashboard";

/** Whether the server has the assistant set up, and with which model. */
export type AssistantInfo = { enabled: boolean; model?: string };

/** What is remembered between visits, per user. */
type Saved = { designOpen: boolean; chatWidth: number; lastChatId: string | null };

const DEFAULTS: Saved = { designOpen: true, chatWidth: CHAT_DEFAULT_WIDTH, lastChatId: null };

type AssistantState = {
  /** null until the server has answered. */
  info: AssistantInfo | null;
  /** The user's chats, most recent first; null until loaded. */
  chats: Chat[] | null;
  refreshChats: () => void;

  /** The chat the page is showing, from its address; null on any other page. */
  chatId: string | null;
  /** The chat most recently shown, for finding the way back to it. */
  lastChatId: string | null;

  /** Whether the panel is showing next to the chat. */
  designOpen: boolean;
  setDesignOpen: (open: boolean) => void;
  /** Which of its tabs is showing. Each chat starts on the graph. */
  paneTab: PaneTab;
  setPaneTab: (tab: PaneTab) => void;
  /** True when the panel sits beside the chat; false when it covers it. */
  beside: boolean;
  /** How wide the chat is while the panel is beside it. */
  chatWidth: number;
  setChatWidth: (width: number) => void;
};

const AssistantContext = createContext<AssistantState | null>(null);

function load(key: string): Saved {
  try {
    const saved = JSON.parse(localStorage.getItem(key) ?? "null") as Partial<Saved> | null;
    return {
      designOpen: saved?.designOpen ?? DEFAULTS.designOpen,
      chatWidth: saved?.chatWidth ?? DEFAULTS.chatWidth,
      lastChatId: saved?.lastChatId ?? DEFAULTS.lastChatId,
    };
  } catch {
    return DEFAULTS;
  }
}

export function clampWidth(width: number) {
  return Math.round(Math.min(CHAT_MAX_WIDTH, Math.max(CHAT_MIN_WIDTH, width)));
}

/** The address of a chat's page, and of the page a new chat starts on. */
export function chatHref(id: string | null) {
  return id ? `/chats/${id}` : "/";
}

function chatIdFrom(pathname: string) {
  return pathname.match(/^\/chats\/([^/]+)$/)?.[1] ?? null;
}

/**
 * Everything about the assistant that outlives a single page: the list of
 * chats, whether the server has a model, and how the chat and its design
 * share the page (remembered per user, in this browser).
 */
export function AssistantProvider({ children }: { children: ReactNode }) {
  const user = useUser();
  const key = `coder.assistant.${user.id}`;

  // Read once, when the signed-in part of the app first renders. That only
  // ever happens in the browser, so there is no server-rendered copy for
  // this to disagree with.
  const [saved, setSaved] = useState<Saved>(() => load(key));

  useEffect(() => {
    try {
      localStorage.setItem(key, JSON.stringify(saved));
    } catch {
      // Storage is full or blocked; the layout still works for this visit.
    }
  }, [key, saved]);

  const chatId = chatIdFrom(usePathname());
  // Noted as the page changes, so a machine's page can lead back to the chat
  // the user came from.
  if (chatId && chatId !== saved.lastChatId) setSaved({ ...saved, lastChatId: chatId });

  // The tab belongs to the chat it was chosen in: another chat's panel
  // starts on its graph.
  const [tab, setTab] = useState<{ chatId: string | null; tab: PaneTab }>({ chatId, tab: "graph" });
  const paneTab = tab.chatId === chatId ? tab.tab : "graph";
  const setPaneTab = useCallback((next: PaneTab) => setTab({ chatId, tab: next }), [chatId]);

  const beside = useMediaQuery(BESIDE_QUERY);
  // On a small screen the design covers the chat, so it never opens by
  // itself there, whatever was remembered from a larger one.
  const [coverOpen, setCoverOpen] = useState(false);

  const setDesignOpen = useCallback(
    (designOpen: boolean) => {
      if (beside) setSaved((s) => ({ ...s, designOpen }));
      else setCoverOpen(designOpen);
    },
    [beside],
  );
  const setChatWidth = useCallback(
    (width: number) => setSaved((s) => ({ ...s, chatWidth: clampWidth(width) })),
    [],
  );

  const { chats, refresh: refreshChats } = useChats();

  const [info, setInfo] = useState<AssistantInfo | null>(null);
  const loadInfo = useCallback(async () => {
    try {
      setInfo(await assistantInfo());
    } catch {
      // Left as it was; the next check will try again.
    }
  }, []);
  usePolling(loadInfo, INFO_REFRESH_MS);

  const value = useMemo(
    () => ({
      info,
      chats,
      refreshChats,
      chatId,
      lastChatId: saved.lastChatId,
      designOpen: beside ? saved.designOpen : coverOpen,
      setDesignOpen,
      paneTab,
      setPaneTab,
      beside,
      chatWidth: clampWidth(saved.chatWidth),
      setChatWidth,
    }),
    [
      info,
      chats,
      refreshChats,
      chatId,
      saved,
      beside,
      coverOpen,
      setDesignOpen,
      paneTab,
      setPaneTab,
      setChatWidth,
    ],
  );

  return <AssistantContext.Provider value={value}>{children}</AssistantContext.Provider>;
}

export function useAssistant() {
  const state = useContext(AssistantContext);
  if (!state) throw new Error("useAssistant must be used inside AssistantProvider");
  return state;
}
