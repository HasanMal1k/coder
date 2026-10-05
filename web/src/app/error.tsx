"use client";

import { useEffect } from "react";
import { Brand } from "@/components/brand";
import { Button } from "@/components/ui/button";

/** Shown when a page throws while rendering, in place of a blank screen. */
export default function ErrorPage({
  error,
  reset,
}: {
  error: Error & { digest?: string };
  reset: () => void;
}) {
  useEffect(() => {
    console.error(error);
  }, [error]);

  return (
    <main className="flex flex-1 flex-col items-center justify-center gap-6 px-4 py-10 text-center">
      <Brand size="lg" />
      <div>
        <h1 className="text-xl font-semibold tracking-tight">Something went wrong</h1>
        <p className="mt-1 text-sm text-muted-foreground">
          The page hit an unexpected problem. Your machines are not affected.
        </p>
      </div>
      <Button onClick={reset}>Try again</Button>
    </main>
  );
}
