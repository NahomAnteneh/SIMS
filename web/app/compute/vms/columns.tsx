"use client";

import { createColumnHelper } from "@tanstack/react-table";
import { Monitor, Cpu, Database, HardDrive, ArrowUpDown, Power } from "lucide-react";
import { Button } from "@/components/ui/button";

export interface VirtualMachine {
  id: string;
  name: string;
  clientId: string;
  applicationId: string;
  datacenterId: string;
  vcenterId: string;
  clusterId: string;
  hostId: string;
  ip: string;
  os: string;
  powerState: "powered_on" | "powered_off" | "suspended" | string;
  toolsStatus: "running" | "not_running" | string;
  vcpus: number;
  memoryGB: number;
  storageGB: number;
  cpuUsagePercent: number;
  memoryUsagePercent: number;
  diskUsagePercent: number;
  uptimeDays: number;
  status: "healthy" | "warning" | "critical" | string;
}

const columnHelper = createColumnHelper<VirtualMachine>();

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
        <div className="text-base font-bold text-zinc-50">{info.getValue()}</div>
        <div className="text-zinc-400 text-xs font-medium mt-1 flex items-center gap-1.5">
          <Monitor className="h-3.5 w-3.5" /> {info.row.original.os}
        </div>
      </div>
    ),
  }),

  // IP Address & Host
  columnHelper.accessor("ip", {
    header: "IP & Host",
    cell: (info) => (
      <div>
        <div className="text-sm font-mono font-bold text-zinc-100">{info.getValue()}</div>
        <div className="text-zinc-400 text-xs font-medium mt-1">
          {info.row.original.hostId}
        </div>
      </div>
    ),
  }),

  // Cluster & Datacenter
  columnHelper.accessor("clusterId", {
    header: "Cluster & Site",
    cell: (info) => (
      <div>
        <div className="text-sm font-bold text-zinc-100">{info.getValue()}</div>
        <div className="text-zinc-400 text-xs font-medium mt-1">
          {info.row.original.datacenterId}
        </div>
      </div>
    ),
  }),

  // Compute Spec
  columnHelper.display({
    id: "specs",
    header: "Resources (vCPU / RAM / Disk)",
    cell: (info) => (
      <div>
        <div className="text-sm font-bold text-zinc-100">
          {info.row.original.vcpus} vCPU • {info.row.original.memoryGB} GB RAM
        </div>
        <div className="text-zinc-400 text-xs font-medium mt-1">
          {info.row.original.storageGB} GB Disk
        </div>
      </div>
    ),
  }),

  // CPU Usage
  columnHelper.accessor("cpuUsagePercent", {
    header: "CPU Load",
    cell: (info) => {
      const percent = info.getValue();
      return (
        <div>
          <div className="text-sm font-bold text-zinc-100 flex items-center gap-1.5">
            <Cpu className="h-4 w-4 text-zinc-400" />
            {percent}%
          </div>
          <div className="text-zinc-400 text-xs font-medium mt-1">
            Uptime {info.row.original.uptimeDays}d
          </div>
        </div>
      );
    },
  }),

  // Memory Usage
  columnHelper.accessor("memoryUsagePercent", {
    header: "Memory Load",
    cell: (info) => {
      const percent = info.getValue();
      return (
        <div>
          <div className="text-sm font-bold text-zinc-100 flex items-center gap-1.5">
            <Database className="h-4 w-4 text-zinc-400" />
            {percent}%
          </div>
          <div className="text-zinc-400 text-xs font-medium mt-1">
            Disk: {info.row.original.diskUsagePercent}%
          </div>
        </div>
      );
    },
  }),

  // Power State
  columnHelper.accessor("powerState", {
    header: "Power",
    cell: (info) => {
      const power = info.getValue();
      const isOn = power === "powered_on";
      return (
        <span
          className={`inline-flex items-center gap-1 px-2.5 py-1 rounded-full text-xs font-medium ${
            isOn
              ? "text-emerald-400 bg-emerald-400/10"
              : "text-zinc-500 bg-zinc-800"
          }`}
        >
          <Power className="h-3 w-3" />
          {isOn ? "On" : "Off"}
        </span>
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
          className={`inline-flex items-center px-3 py-1 rounded-full text-xs font-bold capitalize border ${
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
