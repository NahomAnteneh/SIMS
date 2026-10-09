"use client";

import { createColumnHelper } from "@tanstack/react-table";
import { Monitor, ArrowUpDown, Power, Clock } from "lucide-react";
import { Button } from "@/components/ui/button";
import { type VMListItem } from "@/lib/api/types";
import { type DataTableFeatures } from "@/components/ui/data-table-features";

export type VirtualMachine = VMListItem;

const columnHelper = createColumnHelper<DataTableFeatures, VirtualMachine>();

export const vmColumns = [
  // VM Name & Operating System
  columnHelper.accessor("name", {
    header: ({ column }) => (
      <Button
        variant="ghost"
        onClick={() => column.toggleSorting(column.getIsSorted() === "asc")}
      >
        Virtual Machine
        <ArrowUpDown className="ml-2 h-4 w-4" />
      </Button>
    ),
    cell: (info) => (
      <div>
        <div className="text-sm font-bold text-zinc-100 flex items-center gap-2">
          <Monitor className="h-4 w-4 text-yellow-600 shrink-0" />
          <span className="truncate max-w-[220px]">{info.getValue()}</span>
        </div>
        <div className="text-zinc-500 text-xs font-mono mt-0.5 truncate max-w-[220px]">
          {info.row.original.guest_os || info.row.original.moref}
        </div>
      </div>
    ),
  }),

  // IP Address & MoRef
  columnHelper.accessor("guest_ip_address", {
    header: "IP Address",
    cell: (info) => (
      <div>
        <div className="text-xs font-mono font-medium text-zinc-200">
          {info.getValue() || "No IP assigned"}
        </div>
        <div className="text-zinc-500 text-[11px] font-mono mt-0.5">
          {info.row.original.instance_uuid
            ? `${info.row.original.instance_uuid.slice(0, 16)}...`
            : info.row.original.moref}
        </div>
      </div>
    ),
  }),

  // Location / Hierarchy Folder
  columnHelper.accessor("folder_path", {
    header: "Folder Hierarchy",
    cell: (info) => (
      <div className="text-xs font-mono text-zinc-300 truncate max-w-[180px]">
        {info.getValue() || "Root"}
      </div>
    ),
  }),

  // Compute Spec
  columnHelper.display({
    id: "specs",
    header: "Resources",
    cell: (info) => {
      const vcpus = info.row.original.cpu_cores;
      const ramGB = (info.row.original.memory_mb / 1024).toFixed(1);
      return (
        <div className="text-xs font-medium text-zinc-200">
          <span>{vcpus} vCPU</span> • <span>{ramGB} GB RAM</span>
        </div>
      );
    },
  }),

  // Power State
  columnHelper.accessor("power_state", {
    header: "Power",
    cell: (info) => {
      const power = info.getValue();
      const isOn = power === "poweredOn";
      const isSuspended = power === "suspended";
      return (
        <span
          className={`inline-flex items-center gap-1 px-2 py-0.5 rounded-none text-[11px] font-mono font-medium border ${
            isOn
              ? "text-emerald-400 bg-emerald-400/10 border-emerald-500/30"
              : isSuspended
              ? "text-amber-400 bg-amber-400/10 border-amber-500/30"
              : "text-zinc-400 bg-zinc-800 border-zinc-700"
          }`}
        >
          <Power className="h-3 w-3" />
          {isOn ? "Powered On" : isSuspended ? "Suspended" : "Powered Off"}
        </span>
      );
    },
  }),

  // Connection State
  columnHelper.accessor("connection_state", {
    header: "State",
    cell: (info) => {
      const conn = info.getValue();
      const isConnected = conn === "connected";
      return (
        <span
          className={`inline-flex items-center gap-1.5 text-xs font-mono font-medium ${
            isConnected ? "text-emerald-400" : "text-rose-400"
          }`}
        >
          <span
            className={`w-1.5 h-1.5 rounded-none ${
              isConnected ? "bg-emerald-400" : "bg-rose-400"
            }`}
          />
          <span className="capitalize">{conn || "Unknown"}</span>
        </span>
      );
    },
  }),

  // Discovered / First Seen
  columnHelper.accessor("created_at", {
    header: "Discovered",
    cell: (info) => {
      const val = info.getValue();
      if (!val) return <span className="text-zinc-500 text-xs">N/A</span>;
      return (
        <span className="text-zinc-400 text-xs flex items-center gap-1">
          <Clock className="h-3 w-3 text-zinc-500" />
          {new Date(val).toLocaleDateString()}
        </span>
      );
    },
  }),
];
