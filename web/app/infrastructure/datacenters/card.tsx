import React from "react";
import { Server, Layers, Cpu, HardDrive, Network, Monitor } from "lucide-react";
import {
  Card,
  CardHeader,
  CardTitle,
  CardContent,
  CardFooter,
} from "@/components/ui/card";
import { type Datacenter } from "./columns";

interface DatacenterCardProps {
  datacenter: Datacenter;
  onClick?: () => void;
}

export const DatacenterCard: React.FC<DatacenterCardProps> = ({
  datacenter,
  onClick,
}) => {
  const isHealthy = datacenter.status === "healthy" || datacenter.status === "connected";
  const storageTB = datacenter.totalStorageBytes
    ? (datacenter.totalStorageBytes / (1024 * 1024 * 1024 * 1024)).toFixed(1)
    : null;

  return (
    <Card
      onClick={onClick}
      className={`bg-[#0a0a0a] border border-zinc-800/80 text-[#e6e4e0] rounded-none p-0 overflow-hidden font-sans shadow-lg transition-all ${
        onClick ? "hover:border-yellow-600/50 hover:bg-zinc-900/30 cursor-pointer" : ""
      }`}
    >
      {/* Top Header Row */}
      <CardHeader className="p-4 pb-3 border-b border-zinc-800/80 flex-row items-center justify-between space-y-0">
        <div className="flex items-center gap-2">
          <Server className="h-4 w-4 text-yellow-500" />
          <CardTitle className="text-base font-bold tracking-tight text-white">
            {datacenter.name}
          </CardTitle>
        </div>

        <div className="text-xs text-zinc-500 font-mono">
          #{datacenter.moref || datacenter.id.slice(0, 8)}
        </div>
      </CardHeader>

      {/* Main Plain-Text Key-Value Rows */}
      <CardContent className="p-4 space-y-3 text-sm">
        {/* vCenter Row */}
        <div className="flex items-center justify-between">
          <span className="text-zinc-500 font-normal">vCenter</span>
          <span className="font-medium text-zinc-200 text-xs truncate max-w-[200px]">
            {datacenter.vcenterName}
          </span>
        </div>

        {/* Compute (Clusters / Hosts) */}
        <div className="flex items-center justify-between">
          <span className="text-zinc-500 font-normal flex items-center gap-1">
            <Layers className="h-3.5 w-3.5 text-zinc-400" /> Compute
          </span>
          <span className="font-semibold text-zinc-200">
            {datacenter.clusterCount} Clusters • {datacenter.hostCount} Hosts
          </span>
        </div>

        {/* Workloads (VMs) */}
        <div className="flex items-center justify-between">
          <span className="text-zinc-500 font-normal flex items-center gap-1">
            <Monitor className="h-3.5 w-3.5 text-zinc-400" /> Workloads
          </span>
          <span className="font-semibold text-zinc-100">
            {datacenter.vmCount} Virtual Machines
          </span>
        </div>

        {/* Storage */}
        <div className="flex items-center justify-between">
          <span className="text-zinc-500 font-normal flex items-center gap-1">
            <HardDrive className="h-3.5 w-3.5 text-zinc-400" /> Storage Pools
          </span>
          <span className="font-medium text-zinc-300">
            {datacenter.datastoreCount} Datastores {storageTB ? `(${storageTB} TB)` : ""}
          </span>
        </div>

        {/* Networks */}
        <div className="flex items-center justify-between">
          <span className="text-zinc-500 font-normal flex items-center gap-1">
            <Network className="h-3.5 w-3.5 text-zinc-400" /> Networks
          </span>
          <span className="font-medium text-zinc-300">
            {datacenter.networkCount} Port Groups
          </span>
        </div>
      </CardContent>

      {/* Footer Row */}
      <CardFooter className="p-4 pt-2 border-t border-zinc-800/60 flex items-center justify-between text-xs">
        <span className="text-zinc-500 font-mono text-[11px]">
          ID: {datacenter.id.slice(0, 8)}...
        </span>

        {/* Subtle Dark Pill Status */}
        <div
          className={`inline-flex items-center gap-1.5 px-2 py-0.5 rounded-none text-xs font-mono font-medium border ${
            isHealthy
              ? "bg-emerald-500/10 text-emerald-400 border-emerald-500/30"
              : "bg-amber-500/10 text-amber-400 border-amber-500/30"
          }`}
        >
          <span className={`w-1.5 h-1.5 ${isHealthy ? "bg-emerald-400" : "bg-amber-400"}`} />
          <span className="capitalize">{datacenter.status}</span>
        </div>
      </CardFooter>
    </Card>
  );
};