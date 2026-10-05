"use client";

import { useParams } from "next/navigation";
import { ChatPage } from "@/components/assistant/chat-page";

export default function ExistingChatPage() {
  const { id } = useParams<{ id: string }>();

  return <ChatPage chatId={id} />;
}
