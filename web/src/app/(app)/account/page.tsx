"use client";

import { LogOutIcon } from "lucide-react";
import type { ReactNode } from "react";
import { useUser } from "@/components/auth/require-auth";
import { CopyButton } from "@/components/copy-button";
import { PageHeader } from "@/components/shell/page-header";
import { Avatar, AvatarFallback } from "@/components/ui/avatar";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { initials } from "@/lib/format";
import { clearSession } from "@/lib/session";
import { useTitle } from "@/lib/use-title";

export default function AccountPage() {
  useTitle("Account");
  const user = useUser();
  const name = user.display_name || user.email;

  return (
    <>
      <PageHeader>
        <h1 className="truncate text-sm font-medium">Account</h1>
      </PageHeader>

      <div className="min-h-0 flex-1 overflow-y-auto">
        <div className="mx-auto flex w-full max-w-3xl flex-col gap-6 p-4 sm:p-6 lg:p-8">
          <div>
            <h2 className="text-2xl font-semibold tracking-tight">Account</h2>
            <p className="mt-1 text-sm text-muted-foreground">Who you are signed in as.</p>
          </div>

          <Card>
            <CardHeader>
              <div className="flex items-center gap-4">
                <Avatar className="size-14">
                  <AvatarFallback className="border bg-muted text-lg font-semibold text-foreground">
                    {initials(user.display_name, user.email)}
                  </AvatarFallback>
                </Avatar>
                <div className="min-w-0">
                  <CardTitle className="truncate text-lg">{name}</CardTitle>
                  <CardDescription className="truncate">{user.email}</CardDescription>
                </div>
              </div>
            </CardHeader>
            <CardContent>
              <dl className="divide-y">
                <Row label="Name">{user.display_name || "Not set"}</Row>
                <Row label="Email">{user.email}</Row>
                <Row label="Plan">
                  <Badge variant="secondary" className="capitalize">
                    {user.subscription}
                  </Badge>
                </Row>
                <Row label="User ID">
                  <span className="flex min-w-0 items-center gap-1">
                    <code className="min-w-0 truncate font-mono text-[13px]">{user.id}</code>
                    <CopyButton value={user.id} label="Copy user ID" />
                  </span>
                </Row>
              </dl>
            </CardContent>
          </Card>

          <Card>
            <CardHeader>
              <CardTitle>This browser</CardTitle>
              <CardDescription>
                You stay signed in while the app is open. If it has been closed for a while,
                you&apos;ll be asked to sign in again.
              </CardDescription>
            </CardHeader>
            <CardContent>
              <Button variant="outline" onClick={() => clearSession()}>
                <LogOutIcon data-icon="inline-start" />
                Sign out
              </Button>
            </CardContent>
          </Card>
        </div>
      </div>
    </>
  );
}

function Row({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="flex flex-col gap-1 py-3 text-sm first:pt-0 last:pb-0 sm:flex-row sm:items-center sm:gap-4">
      <dt className="shrink-0 text-muted-foreground sm:w-32">{label}</dt>
      <dd className="min-w-0 flex-1">{children}</dd>
    </div>
  );
}
