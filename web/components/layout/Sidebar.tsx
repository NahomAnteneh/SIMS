"use client";

import React, { useState, useEffect } from "react";
import { usePathname, useRouter } from "next/navigation";
import Link from "next/link";
import {
  Monitor,
  Waypoints,
  Bell,
  Building2,
  Server,
  HardDrive,
  Network,
  Database,
  Shield,
  Settings,
  Warehouse,
  LogOut,
} from "lucide-react";
import { simsApi } from "@/lib/api/client";

interface MenuItem {
  name: string;
  icon: React.ComponentType<{ className?: string }>;
  href: string;
  isAlerts?: boolean;
}

interface MenuGroup {
  title: string;
  items: MenuItem[];
}

const menuGroups: MenuGroup[] = [
  {
    title: "INVENTORY & WORKLOADS",
    items: [
      { name: "Virtual Machines", icon: Monitor, href: "/compute/vms" },
      { name: "Topology Graph", icon: Waypoints, href: "/topology" },
      { name: "Notifications & Activity", icon: Bell, href: "/alerts", isAlerts: true },
    ],
  },
  {
    title: "INFRASTRUCTURE",
    items: [
      { name: "Datacenters", icon: Building2, href: "/infrastructure/datacenters" },
      { name: "Clusters & Hosts", icon: Server, href: "/infrastructure/clusters" },
      { name: "Storage Pools", icon: HardDrive, href: "/infrastructure/storage" },
      { name: "Networks", icon: Network, href: "/infrastructure/networking" },
    ],
  },
  {
    title: "ADMINISTRATION",
    items: [
      { name: "Data Sources", icon: Database, href: "/admin/data-sources" },
      { name: "Access Control", icon: Shield, href: "/admin/access" },
      { name: "Settings", icon: Settings, href: "/admin/settings" },
    ],
  },
];

export const Sidebar = () => {
  const pathname = usePathname();
  const router = useRouter();
  const [user, setUser] = useState<{ username: string; role: string } | null>(null);
  const [unreadCount, setUnreadCount] = useState<number>(0);

  const updateUnread = () => {
    try {
      const stored = localStorage.getItem("sims_unread_alerts");
      if (stored !== null) {
        setUnreadCount(parseInt(stored, 10) || 0);
        return;
      }
    } catch {
      // ignore
    }

    // Default fetch from audit stream
    simsApi
      .listAuditEvents({ limit: 10 })
      .then((events) => {
        const count = Math.min(events.length, 5);
        setUnreadCount(count);
        try {
          localStorage.setItem("sims_unread_alerts", count.toString());
        } catch {
          // ignore
        }
      })
      .catch(() => setUnreadCount(0));
  };

  useEffect(() => {
    simsApi
      .getMe()
      .then((u) => {
        setUser({ username: u.username, role: u.role });
      })
      .catch(() => {
        setUser({ username: "admin", role: "admin" });
      });

    updateUnread();

    const handleAlertsRead = () => updateUnread();
    window.addEventListener("sims-alerts-read", handleAlertsRead);
    return () => window.removeEventListener("sims-alerts-read", handleAlertsRead);
  }, []);

  const handleLogout = () => {
    simsApi.logout();
    router.push("/login");
  };

  return (
    <aside className="w-64 border-r border-zinc-900 bg-[#0a0a0a] flex flex-col h-full shrink-0">
      {/* Logo */}
      <div className="h-14 flex items-center px-4 gap-3 shrink-0 border-b border-zinc-900/60">
        <div className="h-6 w-6 rounded bg-yellow-600 flex items-center justify-center">
          <Warehouse className="h-4 w-4 text-black" />
        </div>
        <div className="flex flex-col">
          <span className="text-sm font-semibold text-zinc-100 leading-tight">SIMS</span>
          <span className="text-[10px] text-zinc-500 leading-tight">VM Inventory Engine</span>
        </div>
      </div>

      {/* Navigation */}
      <nav className="flex-1 overflow-y-auto py-4 custom-scrollbar">
        {menuGroups.map((group, idx) => (
          <div key={idx} className="mb-6">
            <h3 className="px-4 text-[10px] font-semibold text-zinc-500 tracking-wider mb-2 uppercase">
              {group.title}
            </h3>
            <ul className="space-y-0.5">
              {group.items.map((item, itemIdx) => {
                const Icon = item.icon;
                const isActive =
                  pathname === item.href ||
                  (item.href !== "/" && pathname?.startsWith(`${item.href}/`));

                return (
                  <li key={itemIdx}>
                    <Link
                      href={item.href}
                      className={`flex items-center gap-3 px-4 py-2 text-sm relative group transition-colors ${
                        isActive
                          ? "text-zinc-100 bg-zinc-900/60"
                          : "text-zinc-400 hover:text-zinc-200 hover:bg-zinc-900/30"
                      }`}
                    >
                      {isActive && (
                        <div className="absolute left-0 top-0 bottom-0 w-0.5 bg-yellow-500" />
                      )}
                      <Icon className={`h-4 w-4 ${isActive ? "text-yellow-500" : "text-zinc-500 group-hover:text-zinc-300"}`} />
                      <span className="truncate">{item.name}</span>
                      {item.isAlerts && unreadCount > 0 && (
                        <span className="ml-auto text-[10px] font-mono font-medium bg-yellow-500/10 text-yellow-500 border border-yellow-500/20 px-1.5 py-0.2 rounded-full">
                          {unreadCount}
                        </span>
                      )}
                    </Link>
                  </li>
                );
              })}
            </ul>
          </div>
        ))}
      </nav>

      {/* User Profile */}
      <div className="p-3 border-t border-zinc-900 flex items-center justify-between hover:bg-zinc-900/40 shrink-0">
        <div className="flex items-center gap-2.5 min-w-0">
          <div className="h-8 w-8 rounded-full bg-yellow-600/20 text-yellow-500 border border-yellow-600/40 flex items-center justify-center text-xs font-bold uppercase shrink-0">
            {user?.username ? user.username.slice(0, 2) : "AD"}
          </div>
          <div className="flex flex-col min-w-0">
            <span className="text-sm font-medium text-zinc-200 truncate leading-tight">
              {user?.username || "admin"}
            </span>
            <span className="text-[11px] text-zinc-500 capitalize leading-tight">
              {user?.role || "admin"}
            </span>
          </div>
        </div>
        <button
          onClick={handleLogout}
          title="Sign Out"
          className="p-1.5 rounded-md hover:bg-zinc-800 text-zinc-400 hover:text-rose-400 transition-colors"
        >
          <LogOut className="h-4 w-4" />
        </button>
      </div>
    </aside>
  );
};