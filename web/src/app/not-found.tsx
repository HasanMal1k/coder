import Link from "next/link";
import { Brand } from "@/components/brand";
import { Button } from "@/components/ui/button";

export default function NotFound() {
  return (
    <main className="flex flex-1 flex-col items-center justify-center gap-6 px-4 py-10 text-center">
      <Brand size="lg" />
      <div>
        <h1 className="text-xl font-semibold tracking-tight">Page not found</h1>
        <p className="mt-1 text-sm text-muted-foreground">There&apos;s nothing at this address.</p>
      </div>
      <Button asChild>
        <Link href="/">Go to the start</Link>
      </Button>
    </main>
  );
}
