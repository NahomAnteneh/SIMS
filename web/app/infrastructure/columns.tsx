"use client";

import { createColumnHelper } from "@tanstack/react-table";
import {
  Server,
  MapPin,
  Cpu,
  Database,
  HardDrive,
  Layers,
  Network,
  ShieldCheck,
  Zap,
} from "lucide-react";

import { type DataTableFeatures } from "./data-table-features";
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

export interface Cluster {
  id: string;
  name: string;
  vcenterId: string;
  datacenterId: string;
  haEnabled: boolean;
  drsEnabled: boolean;
  status: "healthy" | "warning" | "critical" | string;
  hostCount: number;
  vmCount: number;
  totalCpuGHz: number;
  usedCpuGHz: number;
  totalMemoryGB: number;
  usedMemoryGB: number;
}

export interface Storage {
  id: string;
  name: string;
  datacenterId: string;
  type: string;
  capacityGB: number;
  usedGB: number;
  freeGB: number;
  status: "healthy" | "warning" | "critical" | string;
}

export interface NetworkItem {
  id: string;
  name: string;
  datacenterId: string;
  type: string;
  vlanId: number;
  subnet: string;
  status: "active" | "inactive" | string;
}

import { ArrowUpDown } from "lucide-react";

// 2. Initialize the column helper for the Datacenter type
const columnHelper = createColumnHelper<DataTableFeatures, Datacenter>();
const clusterHelper = createColumnHelper<DataTableFeatures, Cluster>();
const storageHelper = createColumnHelper<DataTableFeatures, Storage>();
const netHelper = createColumnHelper<DataTableFeatures, NetworkItem>();
// 3. Define the columns
export const dcColumns = [
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
      );
    },
    cell: (info) => (
      <div>
        <div className="text-base font-bold text-zinc-50">
          {info.getValue()}
        </div>
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
          {info.row.original.siteCode} •{" "}
          {info.row.original.environment.toUpperCase()}
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
          {info.row.original.vcenterCount} vCenter
          {info.row.original.vcenterCount > 1 ? "s" : ""}
        </div>
        <div className="text-zinc-400 text-xs font-medium mt-1">
          {info.row.original.clusterCount} Clusters /{" "}
          {info.row.original.hostCount} Hosts
        </div>
      </div>
    ),
  }),

  // Workloads (VMs & Apps)
  columnHelper.accessor("vmCount", {
    header: "Workloads (VMs / Apps)",
    cell: (info) => (
      <div>
        <div className="text-sm font-bold text-zinc-100">
          {info.getValue()} VMs
        </div>
        <div className="text-zinc-400 text-xs font-medium mt-1">
          {info.row.original.applicationCount} Apps •{" "}
          {info.row.original.clientCount} Clients
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

export const clusterColumns = [
  // Cluster Name & Identity
  clusterHelper.accessor("name", {
    header: ({ column }) => (
      <Button
        variant="ghost"
        onClick={() => column.toggleSorting(column.getIsSorted() === "asc")}
      >
        Cluster Name
        <ArrowUpDown className="ml-2 h-4 w-4" />
      </Button>
    ),
    cell: (info) => (
      <div>
        <div className="text-base font-bold text-zinc-50">{info.getValue()}</div>
        <div className="text-zinc-400 text-xs font-medium mt-1 flex items-center gap-1.5">
          <Layers className="h-3.5 w-3.5" /> {info.row.original.datacenterId} • {info.row.original.vcenterId}
        </div>
      </div>
    ),
  }),

  // Features (HA & DRS)
  clusterHelper.display({
    id: "features",
    header: "Features (HA / DRS)",
    cell: (info) => {
      const { haEnabled, drsEnabled } = info.row.original;
      return (
        <div>
          <div className="text-sm font-bold text-zinc-100 flex items-center gap-2">
            <span className={haEnabled ? "text-emerald-400" : "text-zinc-500"}>
              HA {haEnabled ? "ON" : "OFF"}
            </span>
            <span className="text-zinc-600">•</span>
            <span className={drsEnabled ? "text-emerald-400" : "text-zinc-500"}>
              DRS {drsEnabled ? "ON" : "OFF"}
            </span>
          </div>
          <div className="text-zinc-400 text-xs font-medium mt-1 flex items-center gap-1">
            <ShieldCheck className="h-3.5 w-3.5 text-zinc-400" /> High Availability & DRS
          </div>
        </div>
      );
    },
  }),

  // Resources (Hosts & VMs)
  clusterHelper.accessor("vmCount", {
    header: "Workloads (Hosts / VMs)",
    cell: (info) => (
      <div>
        <div className="text-sm font-bold text-zinc-100">
          {info.row.original.hostCount} Hosts
        </div>
        <div className="text-zinc-400 text-xs font-medium mt-1">
          {info.getValue()} Active VMs
        </div>
      </div>
    ),
  }),

  // CPU Usage
  clusterHelper.accessor("usedCpuGHz", {
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
  clusterHelper.accessor("usedMemoryGB", {
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

  // Health Status Badge
  clusterHelper.accessor("status", {
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
export const storageColumns = [
  // Datastore Name & Identity
  storageHelper.accessor("name", {
    header: ({ column }) => (
      <Button
        variant="ghost"
        onClick={() => column.toggleSorting(column.getIsSorted() === "asc")}
      >
        Datastore Name
        <ArrowUpDown className="ml-2 h-4 w-4" />
      </Button>
    ),
    cell: (info) => (
      <div>
        <div className="text-base font-bold text-zinc-50">{info.getValue()}</div>
        <div className="text-zinc-400 text-xs font-medium mt-1 flex items-center gap-1.5">
          <HardDrive className="h-3.5 w-3.5" /> {info.row.original.datacenterId}
        </div>
      </div>
    ),
  }),

  // Type & Format
  storageHelper.accessor("type", {
    header: "Type & Format",
    cell: (info) => (
      <div>
        <div className="text-sm font-bold text-zinc-100">{info.getValue()}</div>
        <div className="text-zinc-400 text-xs font-medium mt-1 flex items-center gap-1">
          <Zap className="h-3.5 w-3.5 text-zinc-400" /> Shared Storage
        </div>
      </div>
    ),
  }),

  // Capacity & Utilization
  storageHelper.accessor("usedGB", {
    header: "Capacity Usage",
    cell: (info) => {
      const usedGB = info.getValue();
      const totalGB = info.row.original.capacityGB;
      const freeGB = info.row.original.freeGB;
      const percent = Math.round((usedGB / totalGB) * 100);

      // Convert GB to TB for cleaner text if > 1000 GB
      const usedTB = (usedGB / 1024).toFixed(1);
      const totalTB = (totalGB / 1024).toFixed(1);

      return (
        <div>
          <div className="text-sm font-bold text-zinc-100">
            {usedTB} TB / {totalTB} TB ({percent}%)
          </div>
          <div className="text-zinc-400 text-xs font-medium mt-1">
            {(freeGB / 1024).toFixed(1)} TB Available
          </div>
        </div>
      );
    },
  }),

  // Health Status Badge
  storageHelper.accessor("status", {
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

export const netColumns = [
  // Network Name & Identity
  netHelper.accessor("name", {
    header: ({ column }) => (
      <Button
        variant="ghost"
        onClick={() => column.toggleSorting(column.getIsSorted() === "asc")}
      >
        Network Name
        <ArrowUpDown className="ml-2 h-4 w-4" />
      </Button>
    ),
    cell: (info) => (
      <div>
        <div className="text-base font-bold text-zinc-50">{info.getValue()}</div>
        <div className="text-zinc-400 text-xs font-medium mt-1 flex items-center gap-1.5">
          <Network className="h-3.5 w-3.5" /> {info.row.original.datacenterId}
        </div>
      </div>
    ),
  }),

  // Type & Identifier
  netHelper.accessor("type", {
    header: "Type / VLAN ID",
    cell: (info) => (
      <div>
        <div className="text-sm font-bold text-zinc-100">
          {info.getValue()} (VLAN {info.row.original.vlanId})
        </div>
        <div className="text-zinc-400 text-xs font-medium mt-1">
          Standard Port Group
        </div>
      </div>
    ),
  }),

  // Subnet Range
  netHelper.accessor("subnet", {
    header: "Subnet CIDR",
    cell: (info) => (
      <div>
        <div className="text-sm font-mono font-bold text-zinc-100">
          {info.getValue()}
        </div>
        <div className="text-zinc-400 text-xs font-medium mt-1">
          IPv4 Gateway
        </div>
      </div>
    ),
  }),

  // Active / Inactive Status Badge
  netHelper.accessor("status", {
    header: "Status",
    cell: (info) => {
      const status = info.getValue();
      const isActive = status === "active";

      return (
        <span
          className={`inline-flex items-center px-3 py-1 rounded-full text-xs font-bold capitalize border ${
            isActive
              ? "bg-emerald-500/15 text-emerald-400 border-emerald-500/30"
              : "bg-zinc-500/15 text-zinc-400 border-zinc-500/30"
          }`}
        >
          {status}
        </span>
      );
    },
  }),
];

export interface Host {
  id: string;
  name: string;
  ip: string;
  vcenterId: string;
  clusterId: string;
  datacenterId: string;
  version: string;
  model: string;
  manufacturer: string;
  cpuModel: string;
  physicalCores: number;
  logicalProcessors: number;
  cpuGHz: number;
  memoryGB: number;
  cpuUsagePercent: number;
  memoryUsagePercent: number;
  vmCount: number;
  status: "connected" | "disconnected" | "maintenance" | string;
  uptimeDays: number;
}

const hostHelper = createColumnHelper<DataTableFeatures, Host>();

export const hostColumns = [
  // Host Name & Model
  hostHelper.accessor("name", {
    header: ({ column }) => (
      <Button
        variant="ghost"
        onClick={() => column.toggleSorting(column.getIsSorted() === "asc")}
      >
        Host Name
        <ArrowUpDown className="ml-2 h-4 w-4" />
      </Button>
    ),
    cell: (info) => (
      <div>
        <div className="text-base font-bold text-zinc-50">{info.getValue()}</div>
        <div className="text-zinc-400 text-xs font-medium mt-1 flex items-center gap-1.5">
          <Server className="h-3.5 w-3.5" /> {info.row.original.model}
        </div>
      </div>
    ),
  }),

  // Network & IP
  hostHelper.accessor("ip", {
    header: "IP & ESXi Version",
    cell: (info) => (
      <div>
        <div className="text-sm font-mono font-bold text-zinc-100">{info.getValue()}</div>
        <div className="text-zinc-400 text-xs font-medium mt-1">
          ESXi v{info.row.original.version}
        </div>
      </div>
    ),
  }),

  // Cluster & DC
  hostHelper.accessor("clusterId", {
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

  // CPU Cores & Usage
  hostHelper.accessor("cpuUsagePercent", {
    header: "CPU Utilization",
    cell: (info) => {
      const percent = info.getValue();
      const totalGHz = info.row.original.cpuGHz;
      const cores = info.row.original.physicalCores;
      return (
        <div>
          <div className="text-sm font-bold text-zinc-100 flex items-center gap-1.5">
            <Cpu className="h-4 w-4 text-zinc-400" />
            {totalGHz} GHz ({cores} cores)
          </div>
          <div className="text-zinc-400 text-xs font-medium mt-1">
            {percent}% Allocated
          </div>
        </div>
      );
    },
  }),

  // Memory Usage
  hostHelper.accessor("memoryUsagePercent", {
    header: "Memory Utilization",
    cell: (info) => {
      const percent = info.getValue();
      const totalGB = info.row.original.memoryGB;
      return (
        <div>
          <div className="text-sm font-bold text-zinc-100 flex items-center gap-1.5">
            <Database className="h-4 w-4 text-zinc-400" />
            {totalGB} GB RAM
          </div>
          <div className="text-zinc-400 text-xs font-medium mt-1">
            {percent}% Allocated
          </div>
        </div>
      );
    },
  }),

  // VMs
  hostHelper.accessor("vmCount", {
    header: "Workloads",
    cell: (info) => (
      <div>
        <div className="text-sm font-bold text-zinc-100">{info.getValue()} VMs</div>
        <div className="text-zinc-400 text-xs font-medium mt-1">
          Uptime {info.row.original.uptimeDays}d
        </div>
      </div>
    ),
  }),

  // Status Badge
  hostHelper.accessor("status", {
    header: "Status",
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
];