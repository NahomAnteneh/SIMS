"use client";

import { usePathname } from 'next/navigation';
import Link from 'next/link';
import { 
  LayoutGrid, Waypoints, Bell, 
  Building2, Server, HardDrive, Network, 
  Monitor, Component, Copy, 
  Activity, TrendingUp, BarChart3, 
  Database, Shield, Settings, Warehouse, ChevronDown
} from 'lucide-react';

// Notice we removed the static 'active: boolean' property from these items
const menuGroups = [
  {
    title: "GLOBAL OBSERVABILITY",
    items: [
      { name: "Overview Dashboard", icon: LayoutGrid, href: "/dashboard" },
      { name: "Topology View", icon: Waypoints, href: "/topology" },
      { name: "Alerts & Events", icon: Bell, href: "/alerts", badge: 3 },
    ]
  },
  {
    title: "INFRASTRUCTURE",
    items: [
      { name: "Datacenters", icon: Building2, href: "/infrastructure/datacenters" },
      { name: "Clusters & Hosts", icon: Server, href: "/infrastructure/clusters" },
      { name: "Storage", icon: HardDrive, href: "/infrastructure/storage" },
      { name: "Networking", icon: Network, href: "/infrastructure/networking" },
    ]
  },
  {
    title: "COMPUTE & WORKLOADS",
    items: [
      { name: "Virtual Machines", icon: Monitor, href: "/compute/vms" },
      { name: "Applications & Services", icon: Component, href: "/compute/applications" },
      { name: "Templates", icon: Copy, href: "/compute/templates" },
    ]
  },
  {
    title: "OPERATIONS & ANALYTICS",
    items: [
      { name: "Task Console", icon: Activity, href: "/operations/tasks" },
      { name: "Capacity Planning", icon: TrendingUp, href: "/operations/capacity" },
      { name: "Reports", icon: BarChart3, href: "/operations/reports" },
    ]
  },
  {
    title: "ADMINISTRATION",
    items: [
      { name: "Data Sources", icon: Database, href: "/admin/data-sources" },
      { name: "Access Control", icon: Shield, href: "/admin/access" },
      { name: "Settings", icon: Settings, href: "/admin/settings" },
    ]
  }
];

export const Sidebar = () => {
  const pathname = usePathname(); // Get the current active URL path

  return (
    <aside className="w-64 border-r border-zinc-900 bg-[#0a0a0a] flex flex-col h-full shrink-0">
      {/* Logo */}
      <div className="h-14 flex items-center px-4 gap-3 shrink-0">
        <div className="h-6 w-6 rounded bg-yellow-600 flex items-center justify-center">
          <Warehouse className="h-4 w-4 text-black" />
        </div>
        <div className="flex flex-col">
          <span className="text-sm font-semibold text-zinc-100 leading-tight">Depot</span>
          <span className="text-[10px] text-zinc-500 leading-tight">Inventory Management</span>
        </div>
      </div>

      {/* Navigation */}
      <nav className="flex-1 overflow-y-auto py-4 custom-scrollbar">
        {menuGroups.map((group, idx) => (
          <div key={idx} className="mb-6">
            <h3 className="px-4 text-[10px] font-semibold text-zinc-600 tracking-wider mb-2">
              {group.title}
            </h3>
            <ul className="space-y-0.5">
              {group.items.map((item, itemIdx) => {
                const Icon = item.icon;
                
                // Check if the current pathname matches the item's href
                // Use startsWith if you want parent menus to stay active when on child pages
                const isActive = pathname === item.href || pathname?.startsWith(`${item.href}/`);

                return (
                  <li key={itemIdx}>
                    <Link 
                      href={item.href} 
                      className={`flex items-center gap-3 px-4 py-2 text-sm relative group
                        ${isActive 
                          ? 'text-zinc-100 bg-zinc-900/50' 
                          : 'text-zinc-400 hover:text-zinc-200 hover:bg-zinc-900/30'}`}
                    >
                      {isActive && (
                        <div className="absolute left-0 top-0 bottom-0 w-0.5 bg-yellow-600" />
                      )}
                      <Icon className="h-4 w-4" />
                      <span>{item.name}</span>
                      {item.badge && (
                        <span className="ml-auto text-[10px] font-medium bg-zinc-800 px-1.5 py-0.5 rounded text-zinc-300">
                          {item.badge}
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
      <div className="p-4 border-t border-zinc-900 flex items-center justify-between cursor-pointer hover:bg-zinc-900/50 shrink-0">
        <div className="flex items-center gap-3">
          <div className="h-8 w-8 rounded-full bg-pink-700 flex items-center justify-center text-xs font-medium">
            SC
          </div>
          <div className="flex flex-col">
            <span className="text-sm font-medium text-zinc-200 leading-tight">Sarah Chen</span>
            <span className="text-[11px] text-zinc-500 leading-tight">Admin</span>
          </div>
        </div>
        <ChevronDown className="h-4 w-4 text-zinc-500" />
      </div>
    </aside>
  );
};