"use client";

import { createColumnHelper } from "@tanstack/react-table";
import { Component, ArrowUpDown, ShieldAlert, Building2 } from "lucide-react";
import { Button } from "@/components/ui/button";

export interface Application {
  id: string;
  name: string;
  clientId: string;
  environment: string;
  criticality: "critical" | "high" | "medium" | "low" | string;
  status: "healthy" | "warning" | "critical" | string;
  vmCount: number;
  datacenterCount: number;
  owner: string;
}

import { type DataTableFeatures } from "@/components/ui/data-table-features";

const columnHelper = createColumnHelper<DataTableFeatures, Application>();

export const appColumns = [
  // Application Name & Owner
  columnHelper.accessor("name", {
    header: ({ column }) => (
      <Button
        variant="ghost"
        onClick={() => column.toggleSorting(column.getIsSorted() === "asc")}
      >
        Application Name
        <ArrowUpDown className="ml-2 h-4 w-4" />
      </Button>
    ),
    cell: (info) => (
      <div>
        <div className="text-base font-bold text-zinc-50">{info.getValue()}</div>
        <div className="text-zinc-400 text-xs font-medium mt-1 flex items-center gap-1.5">
          <Component className="h-3.5 w-3.5 text-indigo-400" /> Owner: {info.row.original.owner}
        </div>
      </div>
    ),
  }),

  // Client / Organization
  columnHelper.accessor("clientId", {
    header: "Client / Tenant",
    cell: (info) => (
      <div>
        <div className="text-sm font-bold text-zinc-100">{info.getValue()}</div>
        <div className="text-zinc-400 text-xs font-medium mt-1">Tenant Account</div>
      </div>
    ),
  }),

  // Environment
  columnHelper.accessor("environment", {
    header: "Environment",
    cell: (info) => {
      const env = info.getValue();
      return (
        <span className="inline-flex items-center px-2.5 py-1 rounded-md text-xs font-medium uppercase tracking-wider bg-zinc-800 text-zinc-300 border border-zinc-700/60">
          {env}
        </span>
      );
    },
  }),

  // Criticality
  columnHelper.accessor("criticality", {
    header: "Criticality Tier",
    cell: (info) => {
      const crit = info.getValue();
      const isCritical = crit === "critical";
      const isHigh = crit === "high";

      return (
        <div className="flex items-center gap-1.5 text-xs font-semibold">
          <ShieldAlert
            className={`h-4 w-4 ${
              isCritical
                ? "text-rose-400"
                : isHigh
                ? "text-amber-400"
                : "text-zinc-400"
            }`}
          />
          <span
            className={`capitalize ${
              isCritical
                ? "text-rose-400"
                : isHigh
                ? "text-amber-400"
                : "text-zinc-300"
            }`}
          >
            {crit}
          </span>
        </div>
      );
    },
  }),

  // Scale / Footprint
  columnHelper.display({
    id: "footprint",
    header: "Footprint (VMs / DCs)",
    cell: (info) => (
      <div>
        <div className="text-sm font-bold text-zinc-100">
          {info.row.original.vmCount} Virtual Machines
        </div>
        <div className="text-zinc-400 text-xs font-medium mt-1 flex items-center gap-1">
          <Building2 className="h-3 w-3" /> Across {info.row.original.datacenterCount} Datacenters
        </div>
      </div>
    ),
  }),

  // Status Badge
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
