"use client";

import { createColumnHelper } from "@tanstack/react-table";
import { Server, MapPin, Cpu, Database, HardDrive } from "lucide-react";

import { type DataTableFeatures } from "./data-table-features"
import { Button } from "@/components/ui/button";

// 1. Define the type based on your data structure
export type Datacenter = {
  id: string;
  name: string;
  displayName: string;
  location: string;
  country: string;
  siteCode: string;
  environment: string;
  status: "healthy" | "warning" | "critical" | string;
  vcenterCount: number;
  clusterCount: number;
  hostCount: number;
  vmCount: number;
  applicationCount: number;
  clientCount: number;
  totalCpuGHz: number;
  usedCpuGHz: number;
  totalMemoryGB: number;
  usedMemoryGB: number;
  totalStorageTB: number;
  usedStorageTB: number;
};

import { ArrowUpDown } from "lucide-react"

// 2. Initialize the column helper for the Datacenter type
const columnHelper = createColumnHelper<DataTableFeatures, Datacenter>();

// 3. Define the columns
export const columns = [
  // Datacenter Name & Identity
  columnHelper.accessor("displayName", {
    header: ({ column }) => {
      return (
        <Button
          variant="ghost"
          onClick={() => column.toggleSorting(column.getIsSorted() === "asc")}
        >
          Email
          <ArrowUpDown className="ml-2 h-4 w-4" />
        </Button>
      )
    },
    cell: (info) => (
      <div>
        <div className="text-base font-bold text-zinc-50">{info.getValue()}</div>
        <div className="text-zinc-400 text-xs font-medium mt-1 flex items-center gap-1.5">
          <Server className="h-3.5 w-3.5" /> {info.row.original.name}
        </div>
      </div>
    ),
  }),

  // Location Details
  columnHelper.accessor("location", {
    header: "Location & Site",
    cell: (info) => (
      <div>
        <div className="text-sm font-bold text-zinc-100">{info.getValue()}</div>
        <div className="text-zinc-400 text-xs font-medium mt-1 flex items-center gap-1.5">
          <MapPin className="h-3.5 w-3.5 text-zinc-400" />
          {info.row.original.siteCode} • {info.row.original.environment.toUpperCase()}
        </div>
      </div>
    ),
  }),

  // Compute Infrastructure (vCenters & Hosts)
  // We use .display() instead of .accessor() when a column doesn't directly map to a single sorting key
  columnHelper.display({
    id: "compute",
    header: "Compute (vCenter / Hosts)",
    cell: (info) => (
      <div>
        <div className="text-sm font-bold text-zinc-100">
          {info.row.original.vcenterCount} vCenter{info.row.original.vcenterCount > 1 ? "s" : ""}
        </div>
        <div className="text-zinc-400 text-xs font-medium mt-1">
          {info.row.original.clusterCount} Clusters / {info.row.original.hostCount} Hosts
        </div>
      </div>
    ),
  }),

  // Workloads (VMs & Apps)
  columnHelper.accessor("vmCount", {
    header: "Workloads (VMs / Apps)",
    cell: (info) => (
      <div>
        <div className="text-sm font-bold text-zinc-100">{info.getValue()} VMs</div>
        <div className="text-zinc-400 text-xs font-medium mt-1">
          {info.row.original.applicationCount} Apps • {info.row.original.clientCount} Clients
        </div>
      </div>
    ),
  }),

  // CPU Usage
  columnHelper.accessor("usedCpuGHz", {
    header: "CPU Usage",
    cell: (info) => {
      const used = info.getValue();
      const total = info.row.original.totalCpuGHz;
      const percent = Math.round((used / total) * 100);
      
      return (
        <div>
          <div className="text-sm font-bold text-zinc-100 flex items-center gap-1.5">
            <Cpu className="h-4 w-4 text-zinc-400" />
            {used} / {total} GHz
          </div>
          <div className="text-zinc-400 text-xs font-medium mt-1">
            {percent}% Allocated
          </div>
        </div>
      );
    },
  }),

  // Memory Usage
  columnHelper.accessor("usedMemoryGB", {
    header: "Memory Usage",
    cell: (info) => {
      const used = info.getValue();
      const total = info.row.original.totalMemoryGB;
      const percent = Math.round((used / total) * 100);
      
      return (
        <div>
          <div className="text-sm font-bold text-zinc-100 flex items-center gap-1.5">
            <Database className="h-4 w-4 text-zinc-400" />
            {used} / {total} GB
          </div>
          <div className="text-zinc-400 text-xs font-medium mt-1">
            {percent}% Allocated
          </div>
        </div>
      );
    },
  }),

  // Storage Usage
  columnHelper.accessor("usedStorageTB", {
    header: "Storage Usage",
    cell: (info) => {
      const used = info.getValue();
      const total = info.row.original.totalStorageTB;
      const percent = Math.round((used / total) * 100);
      
      return (
        <div>
          <div className="text-sm font-bold text-zinc-100 flex items-center gap-1.5">
            <HardDrive className="h-4 w-4 text-zinc-400" />
            {used} / {total} TB
          </div>
          <div className="text-zinc-400 text-xs font-medium mt-1">
            {percent}% Allocated
          </div>
        </div>
      );
    },
  }),

  // Health Status Badge
  columnHelper.accessor("status", {
    header: "Status",
    cell: (info) => {
      const status = info.getValue();
      return (
        <span
          className={`inline-flex items-center px-2 py-0.5 rounded-none text-[11px] font-mono font-bold capitalize border ${
            status === "healthy"
              ? "bg-emerald-500/15 text-emerald-400 border-emerald-500/30"
              : status === "warning"
              ? "bg-amber-500/15 text-amber-400 border-amber-500/30"
              : "bg-rose-500/15 text-rose-400 border-rose-500/30"
          }`}
        >
          {status}
        </span>
      );
    },
  }),
];