"use client";

import { DownloadIcon, FileIcon, FileTextIcon, ImageIcon, XIcon } from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";
import { toast } from "sonner";
import {
  MAX_ATTACHMENT_BYTES,
  MAX_ATTACHMENTS,
  errorMessage,
  fetchAttachment,
  type Attachment,
} from "@/lib/api";
import { cn } from "@/lib/utils";
import { Button } from "../ui/button";
import { Dialog, DialogContent, DialogDescription, DialogTitle } from "../ui/dialog";
import { Spinner } from "../ui/spinner";

/** Types shown as pictures. SVG among them: in an <img> it runs nothing. */
const PICTURES = ["image/png", "image/jpeg", "image/gif", "image/webp", "image/svg+xml"];

export function isPicture(contentType: string) {
  return PICTURES.includes(contentType.split(";")[0].trim());
}

/** "24 KB". */
export function formatBytes(bytes: number) {
  if (bytes < 1024) return `${bytes} bytes`;
  if (bytes < 1024 * 1024) return `${Math.round(bytes / 1024)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}

// Files never change once uploaded, so each is fetched once per visit.
const loaded = new Map<string, Promise<string>>();

/** A local address for a chat's file, once it has been fetched; null until then. */
function useFileUrl(chatId: string, id: string) {
  const [url, setUrl] = useState<string | null>(null);
  const [failed, setFailed] = useState(false);

  useEffect(() => {
    let current = true;
    let pending = loaded.get(id);
    if (!pending) {
      pending = fetchAttachment(chatId, id).then((blob) => URL.createObjectURL(blob));
      loaded.set(id, pending);
      // A failure is not remembered: the next look tries again.
      pending.catch(() => loaded.delete(id));
    }
    pending.then(
      (address) => current && setUrl(address),
      () => current && setFailed(true),
    );
    return () => {
      current = false;
    };
  }, [chatId, id]);

  return { url, failed };
}

/** Saves a chat's file to the user's computer, under its own name. */
async function download(chatId: string, file: Attachment) {
  try {
    let pending = loaded.get(file.id);
    if (!pending) {
      pending = fetchAttachment(chatId, file.id).then((blob) => URL.createObjectURL(blob));
      loaded.set(file.id, pending);
    }
    const link = document.createElement("a");
    link.href = await pending;
    link.download = file.name;
    link.click();
  } catch (err) {
    loaded.delete(file.id);
    toast.error(`Couldn't download ${file.name}`, { description: errorMessage(err) });
  }
}

/** The files that came with a message: pictures as pictures, the rest as rows to download. */
export function AttachmentList({ chatId, files }: { chatId: string; files: Attachment[] }) {
  if (files.length === 0) return null;

  const pictures = files.filter((file) => isPicture(file.content_type));
  const others = files.filter((file) => !isPicture(file.content_type));

  return (
    <div className="flex flex-col gap-2">
      {pictures.length > 0 && (
        <ul className="flex flex-wrap gap-2" aria-label="Pictures">
          {pictures.map((file) => (
            <li key={file.id}>
              <Picture chatId={chatId} file={file} />
            </li>
          ))}
        </ul>
      )}
      {others.length > 0 && (
        <ul className="flex flex-col gap-1.5" aria-label="Files">
          {others.map((file) => (
            <li key={file.id}>
              <button
                type="button"
                onClick={() => download(chatId, file)}
                className="group/file flex w-full max-w-sm items-center gap-3 rounded-lg bg-card px-3 py-2 text-left text-sm transition-colors outline-none hover:bg-muted focus-visible:ring-[3px] focus-visible:ring-ring/50"
              >
                <FileKind contentType={file.content_type} />
                <span className="min-w-0 flex-1">
                  <span className="block truncate font-medium">{file.name}</span>
                  <span className="block text-xs text-muted-foreground">
                    {formatBytes(file.size)}
                  </span>
                </span>
                <DownloadIcon className="size-4 shrink-0 text-muted-foreground group-hover/file:text-foreground" />
                <span className="sr-only">Download</span>
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

function FileKind({ contentType }: { contentType: string }) {
  const Icon = contentType.startsWith("text/")
    ? FileTextIcon
    : contentType.startsWith("image/")
      ? ImageIcon
      : FileIcon;
  return (
    <span className="grid size-8 shrink-0 place-items-center rounded-md bg-secondary text-muted-foreground">
      <Icon className="size-4" />
    </span>
  );
}

/** A picture in a message, small; chosen, it opens large. */
function Picture({ chatId, file }: { chatId: string; file: Attachment }) {
  const { url, failed } = useFileUrl(chatId, file.id);
  const [open, setOpen] = useState(false);

  return (
    <>
      <button
        type="button"
        onClick={() => setOpen(true)}
        disabled={!url}
        aria-label={`Open ${file.name}`}
        className="block overflow-hidden rounded-lg bg-card outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50"
      >
        {url ? (
          // A local blob address: Next's image component has nothing to optimise here.
          // eslint-disable-next-line @next/next/no-img-element
          <img
            src={url}
            alt={file.name}
            className="block max-h-60 max-w-[min(100%,20rem)] object-contain"
          />
        ) : (
          <span className="grid h-32 w-48 place-items-center text-xs text-muted-foreground">
            {failed ? "Couldn't load this picture" : <Spinner />}
          </span>
        )}
      </button>

      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent className="max-h-[calc(100dvh-2rem)] gap-3 p-3 sm:max-w-[min(64rem,calc(100%-2rem))]">
          <div className="flex items-center gap-3 pr-10 pl-1">
            <DialogTitle className="min-w-0 flex-1 truncate text-sm font-medium">
              {file.name}
            </DialogTitle>
            <DialogDescription className="sr-only">
              {file.content_type}, {formatBytes(file.size)}
            </DialogDescription>
            <Button variant="ghost" size="sm" onClick={() => download(chatId, file)}>
              <DownloadIcon data-icon="inline-start" />
              Download
            </Button>
          </div>
          {url && (
            // eslint-disable-next-line @next/next/no-img-element
            <img
              src={url}
              alt={file.name}
              className="mx-auto block max-h-[calc(100dvh-7rem)] max-w-full rounded-md object-contain"
            />
          )}
        </DialogContent>
      </Dialog>
    </>
  );
}

/** A file chosen to go with the next message. */
export type PendingFile = {
  key: string;
  file: File;
  /** A local address to show it by, for a picture. */
  preview?: string;
  state: "uploading" | "ready" | "failed";
  /** Once uploaded. */
  attachment?: Attachment;
};

let nextKey = 0;

/**
 * The files chosen for the next message. With `upload`, each is uploaded as
 * soon as it is chosen; without it (no chat yet), they wait to be uploaded by
 * whoever sends them.
 */
export function usePendingFiles(upload?: (file: File) => Promise<Attachment>) {
  const [files, setFiles] = useState<PendingFile[]>([]);
  const current = useRef(files);
  useEffect(() => {
    current.current = files;
  }, [files]);

  // Local addresses are let go when the files are.
  useEffect(
    () => () => {
      for (const file of current.current) if (file.preview) URL.revokeObjectURL(file.preview);
    },
    [],
  );

  const add = useCallback(
    (chosen: File[]) => {
      const room = MAX_ATTACHMENTS - current.current.length;
      if (chosen.length > room) {
        toast.error(`A message can carry ${MAX_ATTACHMENTS} files`, {
          description: room > 0 ? `Only the first ${room} were added.` : undefined,
        });
      }

      const accepted: PendingFile[] = [];
      for (const file of chosen.slice(0, Math.max(0, room))) {
        if (file.size === 0) {
          toast.error(`${file.name} is empty`);
          continue;
        }
        if (file.size > MAX_ATTACHMENT_BYTES) {
          toast.error(`${file.name} is too large`, {
            description: `A file can be at most ${MAX_ATTACHMENT_BYTES / (1024 * 1024)} MB.`,
          });
          continue;
        }
        accepted.push({
          key: `file-${nextKey++}`,
          file,
          preview: isPicture(file.type) ? URL.createObjectURL(file) : undefined,
          state: upload ? "uploading" : "ready",
        });
      }
      if (accepted.length === 0) return;
      setFiles((list) => [...list, ...accepted]);

      if (!upload) return;
      for (const pending of accepted) {
        upload(pending.file).then(
          (attachment) =>
            setFiles((list) =>
              list.map((f) => (f.key === pending.key ? { ...f, state: "ready", attachment } : f)),
            ),
          (err) => {
            toast.error(`Couldn't upload ${pending.file.name}`, {
              description: errorMessage(err),
            });
            setFiles((list) =>
              list.map((f) => (f.key === pending.key ? { ...f, state: "failed" } : f)),
            );
          },
        );
      }
    },
    [upload],
  );

  const remove = useCallback((key: string) => {
    setFiles((list) => {
      const gone = list.find((f) => f.key === key);
      if (gone?.preview) URL.revokeObjectURL(gone.preview);
      return list.filter((f) => f.key !== key);
    });
  }, []);

  const clear = useCallback(() => {
    setFiles((list) => {
      for (const file of list) if (file.preview) URL.revokeObjectURL(file.preview);
      return [];
    });
  }, []);

  return {
    files,
    add,
    remove,
    clear,
    uploading: files.some((f) => f.state === "uploading"),
    failed: files.some((f) => f.state === "failed"),
  };
}

/** The files chosen for the next message, above where it is typed, each with a way to take it off. */
export function PendingFiles({
  files,
  onRemove,
}: {
  files: PendingFile[];
  onRemove: (key: string) => void;
}) {
  if (files.length === 0) return null;

  return (
    <ul aria-label="Files to send" className="flex flex-wrap gap-2 px-3 pt-3">
      {files.map((pending) => (
        <li
          key={pending.key}
          className={cn(
            "relative flex max-w-56 items-center gap-2 rounded-lg bg-muted py-1.5 pr-8 pl-1.5 text-xs",
            pending.state === "failed" && "text-destructive",
          )}
        >
          {pending.preview ? (
            // eslint-disable-next-line @next/next/no-img-element
            <img src={pending.preview} alt="" className="size-8 shrink-0 rounded-md object-cover" />
          ) : (
            <FileKind contentType={pending.file.type} />
          )}
          <span className="min-w-0">
            <span className="block truncate font-medium">{pending.file.name}</span>
            <span className="flex items-center gap-1 text-muted-foreground">
              {pending.state === "uploading" && <Spinner className="size-3" />}
              {pending.state === "failed" ? "Didn't upload" : formatBytes(pending.file.size)}
            </span>
          </span>
          <button
            type="button"
            onClick={() => onRemove(pending.key)}
            aria-label={`Remove ${pending.file.name}`}
            className="absolute top-1/2 right-1.5 grid size-5 -translate-y-1/2 place-items-center rounded-sm text-muted-foreground outline-none hover:bg-accent hover:text-foreground focus-visible:ring-[3px] focus-visible:ring-ring/50"
          >
            <XIcon className="size-3.5" />
          </button>
        </li>
      ))}
    </ul>
  );
}
