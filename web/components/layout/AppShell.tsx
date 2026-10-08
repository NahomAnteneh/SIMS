"use client";

import React, { Suspense } from "react";
import { usePathname } from "next/navigation";
import { Sidebar } from "@/components/layout/Sidebar";
import { Topbar } from "@/components/layout/Topbar";

export function AppShell({ children }: { children: React.ReactNode }) {
  const pathname = usePathname();
  const isLoginPage = pathname === "/login";

  if (isLoginPage) {
    return (
      <main className="flex-1 min-h-screen bg-black">
        {children}
      </main>
    );
  }

  return (
    <div className="flex h-screen w-full bg-black text-zinc-100 overflow-hidden antialiased">
      <Sidebar />
      <div className="flex flex-1 flex-col min-w-0 overflow-hidden">
        <Suspense fallback={<header className="h-14 border-b border-zinc-900 bg-[#0a0a0a]" />}>
          <Topbar />
        </Suspense>
        <main className="flex-1 overflow-y-auto bg-[#0a0a0a] p-4 sm:p-5 w-full">
          {children}
        </main>
      </div>
    </div>
  );
}
