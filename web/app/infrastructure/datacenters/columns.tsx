"use client";

import { createColumnHelper } from "@tanstack/react-table";
import { Server, ArrowUpDown, Layers, Cpu, HardDrive, Network, Monitor, ShieldCheck } from "lucide-react";
import { Button } from "@/components/ui/button";
import { type DataTableFeatures } from "@/components/ui/data-table-features";

export interface Datacenter {
  id: string;
  name: string;
  moref: string;
  vcenterId: string;
  vcenterName: string;
  clusterCount: number;
  hostCount: number;
  vmCount: number;
  datastoreCount: number;
  totalStorageBytes: number;
  networkCount: number;
  status: "healthy" | "warning" | "critical" | string;
}

const columnHelper = createColumnHelper<DataTableFeatures, Datacenter>();

export const columns = [
  // Datacenter Name & MoRef
  columnHelper.accessor("name", {
    header: ({ column }) => (
      <Button
        variant="ghost"
        onClick={() => column.toggleSorting(column.getIsSorted() === "asc")}
        className="font-mono text-xs uppercase text-zinc-300 hover:text-white px-0"
      >
        Datacenter
        <ArrowUpDown className="ml-2 h-3.5 w-3.5 text-zinc-500" />
      </Button>
    ),
    cell: (info) => (
      <div>
        <div className="text-sm font-bold text-zinc-100 flex items-center gap-1.5">
          <Server className="h-4 w-4 text-yellow-500 shrink-0" />
          <span>{info.getValue()}</span>
        </div>
        <div className="text-zinc-500 text-xs font-mono mt-0.5">
          MoRef: {info.row.original.moref || "N/A"}
        </div>
      </div>
    ),
  }),

  // Managing vCenter
  columnHelper.accessor("vcenterName", {
    header: "vCenter Instance",
    cell: (info) => (
      <div>
        <div className="text-sm font-medium text-zinc-200">
          {info.getValue() || "Primary vCenter"}
        </div>
        <div className="text-zinc-500 text-[11px] font-mono mt-0.5 truncate max-w-[200px]">
          {info.row.original.vcenterId}
        </div>
      </div>
    ),
  }),

  // Clusters Count
  columnHelper.accessor("clusterCount", {
    header: "Clusters",
    cell: (info) => (
      <div className="flex items-center gap-1.5 text-sm font-semibold text-zinc-200">
        <Layers className="h-3.5 w-3.5 text-amber-400" />
        <span>{info.getValue()}</span>
        <span className="text-xs text-zinc-500 font-normal">
          {info.getValue() === 1 ? "cluster" : "clusters"}
        </span>
      </div>
    ),
  }),

  // ESXi Hosts Count
  columnHelper.accessor("hostCount", {
    header: "ESXi Hosts",
    cell: (info) => (
      <div className="flex items-center gap-1.5 text-sm font-semibold text-zinc-200">
        <Cpu className="h-3.5 w-3.5 text-blue-400" />
        <span>{info.getValue()}</span>
        <span className="text-xs text-zinc-500 font-normal">
          {info.getValue() === 1 ? "host" : "hosts"}
        </span>
      </div>
    ),
  }),

  // Virtual Machines (Workloads)
  columnHelper.accessor("vmCount", {
    header: "Workloads",
    cell: (info) => (
      <div className="flex items-center gap-1.5 text-sm font-semibold text-zinc-100">
        <Monitor className="h-3.5 w-3.5 text-yellow-500" />
        <span>{info.getValue()}</span>
        <span className="text-xs text-zinc-500 font-normal">VMs</span>
      </div>
    ),
  }),

  // Storage Datastores & Capacity
  columnHelper.accessor("datastoreCount", {
    header: "Storage Pools",
    cell: (info) => {
      const bytes = info.row.original.totalStorageBytes || 0;
      const count = info.getValue();
      const tb = bytes > 0 ? (bytes / (1024 * 1024 * 1024 * 1024)).toFixed(1) : null;
      return (
        <div>
          <div className="text-sm font-semibold text-zinc-200 flex items-center gap-1.5">
            <HardDrive className="h-3.5 w-3.5 text-cyan-400" />
            <span>{count} Datastores</span>
          </div>
          {tb && (
            <div className="text-zinc-500 text-xs font-mono mt-0.5">
              {tb} TB Provisioned
            </div>
          )}
        </div>
      );
    },
  }),

  // Networks
  columnHelper.accessor("networkCount", {
    header: "Networks",
    cell: (info) => (
      <div className="flex items-center gap-1.5 text-sm font-medium text-zinc-300">
        <Network className="h-3.5 w-3.5 text-emerald-400" />
        <span>{info.getValue()} Port Groups</span>
      </div>
    ),
  }),

  // Status Badge
  columnHelper.accessor("status", {
    header: "Status",
    cell: (info) => {
      const status = info.getValue() || "healthy";
      const isHealthy = status === "healthy" || status === "connected";
      return (
        <span
          className={`inline-flex items-center gap-1.5 px-2 py-0.5 rounded-none text-xs font-mono font-medium border ${
            isHealthy
              ? "bg-emerald-500/10 text-emerald-400 border-emerald-500/30"
              : "bg-amber-500/10 text-amber-400 border-amber-500/30"
          }`}
        >
          <span className={`w-1.5 h-1.5 ${isHealthy ? "bg-emerald-400" : "bg-amber-400"}`} />
          {status}
        </span>
      );
    },
  }),
];