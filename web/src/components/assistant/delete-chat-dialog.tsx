"use client";

import type { Chat } from "@/lib/api";
import { Button } from "../ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "../ui/dialog";

/**
 * Asks before a chat is deleted. There is no getting one back, so it is not
 * done at a single click. A chat's name needn't be typed, as a machine's
 * must: what is lost is a conversation, not files.
 */
export function DeleteChatDialog({
  chat,
  onConfirm,
  onClose,
}: {
  /** The chat to delete; null keeps the dialog closed. */
  chat: Chat | null;
  onConfirm: (chat: Chat) => void;
  onClose: () => void;
}) {
  return (
    <Dialog open={chat !== null} onOpenChange={(next) => !next && onClose()}>
      <DialogContent className="sm:max-w-md">
        {chat && (
          <>
            <DialogHeader>
              <DialogTitle>Delete chat</DialogTitle>
              <DialogDescription>
                This permanently deletes{" "}
                <strong className="font-semibold wrap-anywhere text-foreground">
                  {chat.title}
                </strong>
                : its messages and its design. Machines it made are kept. It can&apos;t be undone.
              </DialogDescription>
            </DialogHeader>

            <DialogFooter>
              <Button type="button" variant="outline" onClick={onClose}>
                Cancel
              </Button>
              <Button
                type="button"
                variant="destructive"
                onClick={() => {
                  // The sidebar shows it going, so the dialog needn't stay open for it.
                  onClose();
                  onConfirm(chat);
                }}
              >
                Delete chat
              </Button>
            </DialogFooter>
          </>
        )}
      </DialogContent>
    </Dialog>
  );
}
