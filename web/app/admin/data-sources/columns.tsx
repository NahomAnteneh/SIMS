"use client";

import { createColumnHelper } from "@tanstack/react-table";
import { Database, ArrowUpDown, Clock, CheckCircle2, RefreshCw } from "lucide-react";
import { Button } from "@/components/ui/button";

export interface DataSource {
  id: string;
  name: string;
  type: string;
  endpoint: string;
  datacenterId: string;
  status: "connected" | "disconnected" | "syncing" | string;
  lastSync: string;
  syncDurationSeconds: number;
  objectsDiscovered: number;
  version?: string;
  onResync?: (id: string) => void;
  isResyncing?: boolean;
}

import { type DataTableFeatures } from "@/components/ui/data-table-features";

const columnHelper = createColumnHelper<DataTableFeatures, DataSource>();

export const dataSourceColumns = [
  // Source Name & Endpoint
  columnHelper.accessor("name", {
    header: ({ column }) => (
      <Button
        variant="ghost"
        onClick={() => column.toggleSorting(column.getIsSorted() === "asc")}
      >
        Source Name
        <ArrowUpDown className="ml-2 h-4 w-4" />
      </Button>
    ),
    cell: (info) => (
      <div>
        <div className="text-base font-bold text-zinc-50">{info.getValue()}</div>
        <div className="text-zinc-400 text-xs font-mono font-medium mt-1 flex items-center gap-1.5">
          <Database className="h-3.5 w-3.5 text-yellow-500" /> {info.row.original.endpoint}
        </div>
      </div>
    ),
  }),

  // Type / Integration
  columnHelper.accessor("type", {
    header: "Provider Type",
    cell: (info) => (
      <div>
        <div className="text-sm font-bold text-zinc-100">{info.getValue()}</div>
        <div className="text-zinc-400 text-xs font-medium mt-1">Management Plane</div>
      </div>
    ),
  }),

  // Datacenter
  columnHelper.accessor("datacenterId", {
    header: "Datacenter Site",
    cell: (info) => (
      <div>
        <div className="text-sm font-bold text-zinc-100">{info.getValue()}</div>
        <div className="text-zinc-400 text-xs font-medium mt-1">Primary Region</div>
      </div>
    ),
  }),

  // Discovered Objects & Sync Duration
  columnHelper.accessor("objectsDiscovered", {
    header: "Inventory Objects",
    cell: (info) => (
      <div>
        <div className="text-sm font-bold text-zinc-100">
          {info.getValue()} Objects Discovered
        </div>
        <div className="text-zinc-400 text-xs font-medium mt-1 flex items-center gap-1">
          <Clock className="h-3 w-3" /> Sync time: {info.row.original.syncDurationSeconds}s
        </div>
      </div>
    ),
  }),

  // Last Sync
  columnHelper.accessor("lastSync", {
    header: "Last Sync",
    cell: (info) => {
      const val = info.getValue();
      const dateStr = val ? new Date(val).toLocaleString() : "Never";
      return (
        <div>
          <div className="text-sm font-medium text-zinc-200">{dateStr}</div>
          <div className="text-zinc-500 text-xs mt-1">Automated Interval</div>
        </div>
      );
    },
  }),

  // Status Badge
  columnHelper.accessor("status", {
    header: "Connection",
    cell: (info) => {
      const status = info.getValue();
      const isConnected = status === "connected";
      return (
        <span
          className={`inline-flex items-center px-3 py-1 rounded-full text-xs font-bold capitalize border ${
            isConnected
              ? "bg-emerald-500/15 text-emerald-400 border-emerald-500/30"
              : "bg-rose-500/15 text-rose-400 border-rose-500/30"
          }`}
        >
          {status}
        </span>
      );
    },
  }),

  // Actions
  columnHelper.display({
    id: "actions",
    header: "Actions",
    cell: (info) => {
      const ds = info.row.original;
      return (
        <Button
          variant="outline"
          size="sm"
          disabled={ds.isResyncing}
          onClick={() => ds.onResync?.(ds.id)}
          className="text-xs h-8 gap-1.5 border-zinc-800 hover:bg-zinc-800 text-zinc-300 hover:text-white"
        >
          <RefreshCw className={`h-3.5 w-3.5 ${ds.isResyncing ? "animate-spin text-yellow-500" : ""}`} />
          {ds.isResyncing ? "Syncing..." : "Resync"}
        </Button>
      );
    },
  }),
];
