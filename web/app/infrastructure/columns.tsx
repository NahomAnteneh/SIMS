"use client";

import { createColumnHelper } from "@tanstack/react-table";
import Link from "next/link";
import {
  Server,
  Layers,
  Cpu,
  HardDrive,
  Network,
  Monitor,
  ShieldCheck,
  Zap,
  ArrowUpDown,
  Power,
  ChevronRight,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import { type DataTableFeatures } from "@/components/ui/data-table-features";
import { formatVlanDisplay } from "@/lib/vlan";

// 1. Domain Interfaces Grounded in Real Backend Tables
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

export interface Cluster {
  id: string;
  name: string;
  moref: string;
  vcenterId: string;
  vcenterName: string;
  datacenterId: string;
  datacenterName: string;
  haEnabled: boolean;
  drsEnabled: boolean;
  status: "healthy" | "warning" | "critical" | string;
  hostCount: number;
  vmCount: number;
}

export interface Storage {
  id: string;
  name: string;
  moref: string;
  vcenterId: string;
  vcenterName: string;
  datacenterId: string;
  datacenterName: string;
  type: string;
  capacityGB: number;
  usedGB: number;
  freeGB: number;
  vmCount: number;
  status: "healthy" | "warning" | "critical" | string;
}

export interface NetworkItem {
  id: string;
  name: string;
  moref: string;
  vcenterId: string;
  vcenterName: string;
  datacenterId: string;
  datacenterName: string;
  type: string;
  vlanIds?: string;
  switchName?: string;
  vmCount: number;
  status: "active" | "inactive" | string;
}

export interface Host {
  id: string;
  name: string;
  moref: string;
  vcenterId: string;
  vcenterName: string;
  clusterId: string;
  clusterName: string;
  datacenterId: string;
  datacenterName: string;
  vmCount: number;
  powerState: string;
  connectionState: string;
  status: "connected" | "disconnected" | "healthy" | "warning" | string;
}

// 2. Initialize Column Helpers
const columnHelper = createColumnHelper<DataTableFeatures, Datacenter>();
const clusterHelper = createColumnHelper<DataTableFeatures, Cluster>();
const storageHelper = createColumnHelper<DataTableFeatures, Storage>();
const netHelper = createColumnHelper<DataTableFeatures, NetworkItem>();
const hostHelper = createColumnHelper<DataTableFeatures, Host>();

// 3. Datacenter Columns
export const dcColumns = [
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

  // Clusters
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

  // Hosts
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

  // Virtual Machines
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

  // Storage Pools
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

// 4. Cluster Columns
export const clusterColumns = [
  // Cluster Name & MoRef
  clusterHelper.accessor("name", {
    header: ({ column }) => (
      <Button
        variant="ghost"
        onClick={() => column.toggleSorting(column.getIsSorted() === "asc")}
        className="font-mono text-xs uppercase text-zinc-300 hover:text-white px-0"
      >
        Cluster Name
        <ArrowUpDown className="ml-2 h-3.5 w-3.5 text-zinc-500" />
      </Button>
    ),
    cell: (info) => (
      <div>
        <div className="text-sm font-bold text-zinc-100 flex items-center gap-1.5">
          <Layers className="h-4 w-4 text-amber-500 shrink-0" />
          <span>{info.getValue()}</span>
        </div>
        <div className="text-zinc-500 text-xs font-mono mt-0.5">
          MoRef: {info.row.original.moref || "N/A"}
        </div>
      </div>
    ),
  }),

  // Datacenter
  clusterHelper.accessor("datacenterName", {
    header: "Datacenter",
    cell: (info) => (
      <div>
        <div className="text-sm font-medium text-zinc-200">
          {info.getValue() || "Datacenter"}
        </div>
        <div className="text-zinc-500 text-[11px] font-mono mt-0.5 truncate max-w-[150px]">
          {info.row.original.datacenterId}
        </div>
      </div>
    ),
  }),

  // Managing vCenter
  clusterHelper.accessor("vcenterName", {
    header: "vCenter Instance",
    cell: (info) => (
      <div className="text-sm font-medium text-zinc-300 truncate max-w-[180px]">
        {info.getValue() || info.row.original.vcenterId}
      </div>
    ),
  }),

  // Hosts Count
  clusterHelper.accessor("hostCount", {
    header: "ESXi Hosts",
    cell: (info) => (
      <div className="flex items-center gap-1.5 text-sm font-semibold text-zinc-200">
        <Cpu className="h-3.5 w-3.5 text-blue-400" />
        <span>{info.getValue()} Hosts</span>
      </div>
    ),
  }),

  // Workloads (VMs)
  clusterHelper.accessor("vmCount", {
    header: "Workloads",
    cell: (info) => (
      <div className="flex items-center gap-1.5 text-sm font-semibold text-zinc-100">
        <Monitor className="h-3.5 w-3.5 text-yellow-500" />
        <span>{info.getValue()} Active VMs</span>
      </div>
    ),
  }),

  // Features (HA / DRS)
  clusterHelper.display({
    id: "features",
    header: "HA & DRS",
    cell: (info) => {
      const { haEnabled, drsEnabled } = info.row.original;
      return (
        <div className="flex items-center gap-2 text-xs font-mono">
          <span
            className={`px-1.5 py-0.5 border ${
              haEnabled
                ? "bg-emerald-500/10 text-emerald-400 border-emerald-500/30"
                : "bg-zinc-800 text-zinc-500 border-zinc-700"
            }`}
          >
            HA {haEnabled ? "ON" : "OFF"}
          </span>
          <span
            className={`px-1.5 py-0.5 border ${
              drsEnabled
                ? "bg-emerald-500/10 text-emerald-400 border-emerald-500/30"
                : "bg-zinc-800 text-zinc-500 border-zinc-700"
            }`}
          >
            DRS {drsEnabled ? "ON" : "OFF"}
          </span>
        </div>
      );
    },
  }),

  // Status Badge
  clusterHelper.accessor("status", {
    header: "Status",
    cell: (info) => {
      const status = info.getValue() || "healthy";
      return (
        <span className="inline-flex items-center gap-1.5 px-2 py-0.5 rounded-none text-xs font-mono font-medium border bg-emerald-500/10 text-emerald-400 border-emerald-500/30">
          <span className="w-1.5 h-1.5 bg-emerald-400" />
          {status}
        </span>
      );
    },
  }),
];

// 5. Storage / Datastore Columns
export const storageColumns = [
  // Datastore Name & MoRef
  storageHelper.accessor("name", {
    header: ({ column }) => (
      <Button
        variant="ghost"
        onClick={() => column.toggleSorting(column.getIsSorted() === "asc")}
        className="font-mono text-xs uppercase text-zinc-300 hover:text-white px-0"
      >
        Datastore Name
        <ArrowUpDown className="ml-2 h-3.5 w-3.5 text-zinc-500" />
      </Button>
    ),
    cell: (info) => (
      <div>
        <div className="text-sm font-bold text-zinc-100 flex items-center gap-1.5">
          <HardDrive className="h-4 w-4 text-cyan-400 shrink-0" />
          <span>{info.getValue() || "Unnamed Datastore"}</span>
        </div>
        <div className="text-zinc-500 text-xs font-mono mt-0.5">
          MoRef: {info.row.original.moref || "N/A"}
        </div>
      </div>
    ),
  }),

  // Datacenter
  storageHelper.accessor("datacenterName", {
    header: "Datacenter",
    cell: (info) => (
      <div className="text-sm font-medium text-zinc-200">
        {info.getValue() || "Primary Datacenter"}
      </div>
    ),
  }),

  // Managing vCenter
  storageHelper.accessor("vcenterName", {
    header: "vCenter Instance",
    cell: (info) => (
      <div className="text-sm font-medium text-zinc-300 truncate max-w-[180px]">
        {info.getValue() || info.row.original.vcenterId}
      </div>
    ),
  }),

  // Type / Storage Format
  storageHelper.accessor("type", {
    header: "File System",
    cell: (info) => (
      <div className="flex items-center gap-1 text-xs font-mono font-semibold text-zinc-200">
        <Zap className="h-3.5 w-3.5 text-yellow-500" />
        <span>{info.getValue() || "VMFS"}</span>
      </div>
    ),
  }),

  // Capacity & Utilization
  storageHelper.accessor("capacityGB", {
    header: "Capacity Allocation",
    cell: (info) => {
      const capGB = info.getValue() || 0;
      const usedGB = info.row.original.usedGB || 0;
      const freeGB = info.row.original.freeGB || 0;

      if (capGB <= 0) {
        return (
          <div className="text-xs text-zinc-500 font-mono">
            Unreported / Dynamic
          </div>
        );
      }

      const percent = Math.round((usedGB / capGB) * 100);
      const usedTB = (usedGB / 1024).toFixed(1);
      const totalTB = (capGB / 1024).toFixed(1);
      const freeTB = (freeGB / 1024).toFixed(1);

      return (
        <div className="min-w-[160px] space-y-1">
          <div className="text-xs font-mono text-zinc-200 flex justify-between">
            <span>{usedTB} / {totalTB} TB ({percent}%)</span>
            <span className="text-zinc-400">{freeTB} TB Free</span>
          </div>
          <div className="h-1.5 w-full bg-zinc-800 rounded-none overflow-hidden">
            <div
              className={`h-full ${
                percent > 85 ? "bg-rose-500" : percent > 70 ? "bg-amber-500" : "bg-cyan-500"
              }`}
              style={{ width: `${Math.min(percent, 100)}%` }}
            />
          </div>
        </div>
      );
    },
  }),

  // Attached VMs / Workloads
  storageHelper.accessor("vmCount", {
    header: ({ column }) => (
      <Button
        variant="ghost"
        onClick={() => column.toggleSorting(column.getIsSorted() === "asc")}
        className="font-mono text-xs uppercase text-zinc-300 hover:text-white px-0"
      >
        Attached Workloads
        <ArrowUpDown className="ml-2 h-3.5 w-3.5 text-zinc-500" />
      </Button>
    ),
    cell: (info) => (
      <div className="flex items-center gap-1.5 text-sm font-semibold text-zinc-100">
        <Monitor className="h-3.5 w-3.5 text-yellow-500 shrink-0" />
        <span>{info.getValue() || 0} VMs</span>
      </div>
    ),
  }),

  // Status Badge
  storageHelper.accessor("status", {
    header: "Status",
    cell: (info) => {
      const status = info.getValue() || "healthy";
      const isHealthy = status === "healthy";
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

// 6. Network Columns
export const netColumns = [
  // Network Name & MoRef
  netHelper.accessor("name", {
    header: ({ column }) => (
      <Button
        variant="ghost"
        onClick={() => column.toggleSorting(column.getIsSorted() === "asc")}
        className="font-mono text-xs uppercase text-zinc-300 hover:text-white px-0"
      >
        Network / Port Group
        <ArrowUpDown className="ml-2 h-3.5 w-3.5 text-zinc-500" />
      </Button>
    ),
    cell: (info) => (
      <div>
        <div className="text-sm font-bold text-zinc-100 flex items-center gap-1.5">
          <Network className="h-4 w-4 text-emerald-400 shrink-0" />
          <span>{info.getValue() || "Default Network"}</span>
        </div>
        <div className="text-zinc-500 text-xs font-mono mt-0.5">
          MoRef: {info.row.original.moref || "N/A"}
        </div>
      </div>
    ),
  }),

  // Datacenter
  netHelper.accessor("datacenterName", {
    header: "Datacenter",
    cell: (info) => (
      <div className="text-sm font-medium text-zinc-200">
        {info.getValue() || "Primary Datacenter"}
      </div>
    ),
  }),

  // Managing vCenter
  netHelper.accessor("vcenterName", {
    header: "vCenter Instance",
    cell: (info) => (
      <div className="text-sm font-medium text-zinc-300 truncate max-w-[180px]">
        {info.getValue() || info.row.original.vcenterId}
      </div>
    ),
  }),

  // Network Type
  netHelper.accessor("type", {
    header: "Type",
    cell: (info) => (
      <span className="text-xs font-mono font-medium text-zinc-300">
        {info.getValue() || "Standard"}
      </span>
    ),
  }),

  // Distributed Switch / Switch Name
  netHelper.accessor("switchName", {
    header: "Switch / DVS",
    cell: (info) => (
      <span className="text-xs font-mono font-semibold text-zinc-300">
        {info.getValue() || "Standard vSwitch"}
      </span>
    ),
  }),

  // VLAN IDs
  netHelper.accessor("vlanIds", {
    header: "VLAN",
    cell: (info) => {
      const val = info.getValue();
      const display = formatVlanDisplay(val);
      if (display === "None") {
        return <span className="text-xs font-mono text-zinc-500">None</span>;
      }
      return (
        <Link
          href={`/compute/vms?vlan=${encodeURIComponent(display)}`}
          className="inline-block text-xs font-mono font-bold text-amber-400 bg-amber-400/10 px-1.5 py-0.5 border border-amber-500/20 hover:border-amber-400 hover:bg-amber-400/20 transition-colors"
          title={`Filter VMs on VLAN ${display}`}
        >
          VLAN {display}
        </Link>
      );
    },
  }),

  // Attached Workloads (VMs)
  netHelper.accessor("vmCount", {
    header: ({ column }) => (
      <Button
        variant="ghost"
        onClick={() => column.toggleSorting(column.getIsSorted() === "asc")}
        className="font-mono text-xs uppercase text-zinc-300 hover:text-white px-0"
      >
        Attached Workloads
        <ArrowUpDown className="ml-2 h-3.5 w-3.5 text-zinc-500" />
      </Button>
    ),
    cell: (info) => (
      <Link
        href={`/compute/vms?network_id=${info.row.original.id}&network_name=${encodeURIComponent(info.row.original.name)}`}
        className="flex items-center gap-1.5 text-sm font-semibold text-zinc-100 hover:text-yellow-400 transition-colors group"
        title={`View VMs connected to ${info.row.original.name}`}
      >
        <Monitor className="h-3.5 w-3.5 text-yellow-500 shrink-0 group-hover:scale-110 transition-transform" />
        <span className="underline decoration-zinc-700 underline-offset-4 group-hover:decoration-yellow-400">
          {info.getValue() || 0} VMs
        </span>
      </Link>
    ),
  }),

  // Status Badge
  netHelper.accessor("status", {
    header: "Status",
    cell: (info) => {
      const status = info.getValue() || "active";
      const isActive = status === "active";
      return (
        <span
          className={`inline-flex items-center gap-1.5 px-2 py-0.5 rounded-none text-xs font-mono font-medium border ${
            isActive
              ? "bg-emerald-500/10 text-emerald-400 border-emerald-500/30"
              : "bg-zinc-800 text-zinc-400 border-zinc-700"
          }`}
        >
          <span className={`w-1.5 h-1.5 ${isActive ? "bg-emerald-400" : "bg-zinc-400"}`} />
          {status}
        </span>
      );
    },
  }),
];

// 7. Host Columns
export const hostColumns = [
  // Host Name & MoRef
  hostHelper.accessor("name", {
    header: ({ column }) => (
      <Button
        variant="ghost"
        onClick={() => column.toggleSorting(column.getIsSorted() === "asc")}
        className="font-mono text-xs uppercase text-zinc-300 hover:text-white px-0"
      >
        Hypervisor Host
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

  // Cluster
  hostHelper.accessor("clusterName", {
    header: "Cluster",
    cell: (info) => (
      <div>
        <div className="text-sm font-semibold text-zinc-200">
          {info.getValue() || "Standalone"}
        </div>
        <div className="text-zinc-500 text-xs font-mono mt-0.5">
          {info.row.original.datacenterName}
        </div>
      </div>
    ),
  }),

  // Managing vCenter
  hostHelper.accessor("vcenterName", {
    header: "vCenter Instance",
    cell: (info) => (
      <div className="text-sm font-medium text-zinc-300 truncate max-w-[180px]">
        {info.getValue() || info.row.original.vcenterId}
      </div>
    ),
  }),

  // Workloads (VM count)
  hostHelper.accessor("vmCount", {
    header: ({ column }) => (
      <Button
        variant="ghost"
        onClick={() => column.toggleSorting(column.getIsSorted() === "asc")}
        className="font-mono text-xs uppercase text-zinc-300 hover:text-white px-0"
      >
        Workloads
        <ArrowUpDown className="ml-2 h-3.5 w-3.5 text-zinc-500" />
      </Button>
    ),
    cell: (info) => (
      <Link
        href={`/compute/vms?host_id=${info.row.original.id}&host_name=${encodeURIComponent(info.row.original.name)}`}
        onClick={(e) => e.stopPropagation()}
        className="inline-flex items-center gap-1.5 text-sm font-semibold text-yellow-500 hover:text-yellow-400 hover:underline"
        title={`Filter virtual machines on ${info.row.original.name}`}
      >
        <Monitor className="h-3.5 w-3.5 text-yellow-500 shrink-0" />
        <span>{info.getValue() || 0}</span>
        <span className="text-xs text-zinc-400 font-normal">VMs</span>
      </Link>
    ),
  }),

  // Power State
  hostHelper.accessor("powerState", {
    header: "Power State",
    cell: (info) => {
      const power = info.getValue() || "poweredOn";
      const isOn = power === "poweredOn";
      return (
        <span
          className={`inline-flex items-center gap-1.5 px-2 py-0.5 rounded-none text-xs font-mono font-medium border ${
            isOn
              ? "bg-emerald-500/10 text-emerald-400 border-emerald-500/30"
              : "bg-zinc-800 text-zinc-400 border-zinc-700"
          }`}
        >
          <Power className="h-3 w-3" />
          {power}
        </span>
      );
    },
  }),

  // Connection State
  hostHelper.accessor("connectionState", {
    header: "Connection",
    cell: (info) => {
      const conn = info.getValue() || "connected";
      const isConnected = conn === "connected";
      return (
        <span
          className={`inline-flex items-center gap-1.5 text-xs font-mono font-medium ${
            isConnected ? "text-emerald-400" : "text-rose-400"
          }`}
        >
          <span
            className={`w-1.5 h-1.5 ${
              isConnected ? "bg-emerald-400" : "bg-rose-400"
            }`}
          />
          <span className="capitalize">{conn}</span>
        </span>
      );
    },
  }),

  // Action column to directly view host VMs
  hostHelper.display({
    id: "actions",
    header: "Filter",
    cell: (info) => (
      <Link
        href={`/compute/vms?host_id=${info.row.original.id}&host_name=${encodeURIComponent(info.row.original.name)}`}
        onClick={(e) => e.stopPropagation()}
        className="inline-flex items-center gap-1 text-xs font-mono text-zinc-400 hover:text-yellow-400 border border-zinc-800 hover:border-yellow-500/40 px-2 py-1 bg-zinc-900/60 transition-colors"
      >
        View VMs
        <ChevronRight className="h-3 w-3" />
      </Link>
    ),
  }),
];