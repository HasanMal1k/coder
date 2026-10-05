"use client";

import type { ReactNode } from "react";
import { AssistantAgents } from "@/components/agents/agents-section";
import { AssistantProvider } from "@/components/assistant/assistant-provider";
import { RequireAuth } from "@/components/auth/require-auth";
import { AppSidebar } from "@/components/shell/app-sidebar";
import { ShellProvider } from "@/components/shell/shell-provider";
import { SidebarInset, SidebarProvider } from "@/components/ui/sidebar";
import { InstancesProvider } from "@/lib/instances";

/**
 * Everything behind sign-in. The providers and the sidebar live here, above
 * the pages, so their state (the lists of chats and machines, open dialogs,
 * whether the sidebar is collapsed) survives moving from one page to
 * another.
 */
export default function AppLayout({ children }: { children: ReactNode }) {
  return (
    <RequireAuth>
      <InstancesProvider>
        <AssistantProvider>
          <AssistantAgents>
            <ShellProvider>
              <SidebarProvider>
                <AppSidebar />
                {/* A fixed frame the height of the window: each page scrolls
                  inside it, and the editor and terminal can fill it exactly. */}
                <SidebarInset className="h-svh overflow-hidden">
                  {/* A container, so a page lays itself out by the room it
                    actually has, whatever the window and the sidebar leave. */}
                  <div className="@container/page flex min-h-0 min-w-0 flex-1 flex-col">
                    {children}
                  </div>
                </SidebarInset>
              </SidebarProvider>
            </ShellProvider>
          </AssistantAgents>
        </AssistantProvider>
      </InstancesProvider>
    </RequireAuth>
  );
}
