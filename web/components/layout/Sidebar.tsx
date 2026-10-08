"use client";

import React, { useState, useEffect } from "react";
import { usePathname, useRouter } from "next/navigation";
import Link from "next/link";
import {
  LayoutDashboard,
  Monitor,
  GitFork,
  Building2,
  HardDrive,
  Network,
  Database,
  Server,
  Settings,
  Warehouse,
  LogOut,
} from "lucide-react";
import { simsApi } from "@/lib/api/client";

interface SidebarItem {
  name: string;
  icon: React.ComponentType<{ className?: string }>;
  href: string;
}

const navItems: SidebarItem[] = [
  { name: "Overview", icon: LayoutDashboard, href: "/dashboard" },
  { name: "Virtual Machines", icon: Monitor, href: "/compute/vms" },
  { name: "Topology", icon: GitFork, href: "/topology" },
  { name: "Datacenters", icon: Building2, href: "/infrastructure/datacenters" },
  { name: "Hosts", icon: Server, href: "/infrastructure/hosts" },
  { name: "Datastores", icon: HardDrive, href: "/infrastructure/storage" },
  { name: "Networks", icon: Network, href: "/infrastructure/networking" },
  { name: "vCenters", icon: Database, href: "/admin/data-sources" },
  { name: "Settings", icon: Settings, href: "/admin/settings" },
];

export const Sidebar = () => {
  const pathname = usePathname();
  const router = useRouter();
  const [user, setUser] = useState<{ username: string; role: string } | null>(null);

  useEffect(() => {
    simsApi
      .getMe()
      .then((u) => {
        setUser({ username: u.username, role: u.role });
      })
      .catch(() => {
        setUser({ username: "admin", role: "admin" });
      });
  }, []);

  const handleLogout = () => {
    simsApi.logout();
    router.push("/login");
  };

  return (
    <aside className="w-56 border-r border-zinc-900 bg-[#080808] flex flex-col h-full shrink-0 select-none">
      {/* Platform Branding */}
      <div className="h-14 flex items-center px-4 gap-2.5 shrink-0 border-b border-zinc-900 bg-[#0a0a0a]">
        <div className="h-6 w-6 bg-yellow-500 flex items-center justify-center shrink-0">
          <Warehouse className="h-3.5 w-3.5 text-black" />
        </div>
        <div className="flex flex-col min-w-0">
          <span className="text-xs font-bold uppercase tracking-wider text-zinc-100 leading-none">
            SIMS
          </span>
          <span className="text-[10px] text-zinc-500 font-mono leading-tight mt-0.5">
            INFRASTRUCTURE
          </span>
        </div>
      </div>

      {/* Navigation List */}
      <nav className="flex-1 overflow-y-auto py-3">
        <ul className="space-y-0.5 px-2">
          {navItems.map((item) => {
            const Icon = item.icon;
            const isActive =
              pathname === item.href ||
              (item.href !== "/" && pathname?.startsWith(`${item.href}/`));

            return (
              <li key={item.name}>
                <Link
                  href={item.href}
                  className={`flex items-center gap-2.5 px-3 py-2 text-xs font-medium transition-colors ${
                    isActive
                      ? "text-yellow-400 bg-zinc-900 border-l-2 border-yellow-500"
                      : "text-zinc-400 hover:text-zinc-200 hover:bg-zinc-900/50"
                  }`}
                >
                  <Icon
                    className={`h-4 w-4 shrink-0 ${
                      isActive ? "text-yellow-400" : "text-zinc-500"
                    }`}
                  />
                  <span className="truncate">{item.name}</span>
                </Link>
              </li>
            );
          })}
        </ul>
      </nav>

      {/* Operator Session Info */}
      <div className="p-3 border-t border-zinc-900 bg-[#0a0a0a] flex items-center justify-between shrink-0">
        <div className="flex items-center gap-2 min-w-0">
          <div className="h-6 w-6 bg-zinc-800 text-yellow-500 border border-zinc-700 flex items-center justify-center text-[10px] font-mono font-bold uppercase shrink-0">
            {user?.username ? user.username.slice(0, 2).toUpperCase() : "AD"}
          </div>
          <div className="flex flex-col min-w-0">
            <span className="text-xs font-medium text-zinc-200 truncate leading-tight">
              {user?.username || "admin"}
            </span>
            <span className="text-[10px] text-zinc-500 uppercase font-mono leading-tight">
              {user?.role || "admin"}
            </span>
          </div>
        </div>
        <button
          onClick={handleLogout}
          title="Sign Out"
          className="p-1.5 hover:bg-zinc-800 text-zinc-500 hover:text-rose-400 transition-colors"
        >
          <LogOut className="h-3.5 w-3.5" />
        </button>
      </div>
    </aside>
  );
};