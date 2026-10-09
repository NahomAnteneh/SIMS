import React from "react";
import { Layers, ShieldCheck, Cpu, Monitor } from "lucide-react";
import {
  Card,
  CardHeader,
  CardTitle,
  CardContent,
  CardFooter,
} from "@/components/ui/card";
import { type Cluster } from "../columns";

interface ClusterCardProps {
  cluster: Cluster;
  onClick?: () => void;
}

export const ClusterCard: React.FC<ClusterCardProps> = ({ cluster, onClick }) => {
  return (
    <Card
      onClick={onClick}
      className={`bg-[#0a0a0a] border border-zinc-800/80 text-[#e6e4e0] rounded-none p-0 overflow-hidden font-sans shadow-lg transition-all ${
        onClick ? "hover:border-yellow-600/50 hover:bg-zinc-900/30 cursor-pointer" : ""
      }`}
    >
      <CardHeader className="p-4 pb-3 border-b border-zinc-800/80 flex-row items-center justify-between space-y-0">
        <CardTitle className="text-base font-bold tracking-tight text-white flex items-center gap-2">
          <Layers className="h-4 w-4 text-amber-500" />
          {cluster.name}
        </CardTitle>
        <span className="text-xs text-zinc-500 font-mono">
          #{cluster.moref || cluster.id.slice(0, 8)}
        </span>
      </CardHeader>

      <CardContent className="p-4 space-y-3 text-sm">
        <div className="flex items-center justify-between">
          <span className="text-zinc-500 font-normal">Datacenter</span>
          <span className="font-semibold text-zinc-200">
            {cluster.datacenterName || "Datacenter"}
          </span>
        </div>

        <div className="flex items-center justify-between">
          <span className="text-zinc-500 font-normal">vCenter</span>
          <span className="font-medium text-zinc-300 text-xs truncate max-w-[200px]">
            {cluster.vcenterName || cluster.vcenterId}
          </span>
        </div>

        <div className="flex items-center justify-between">
          <span className="text-zinc-500 font-normal flex items-center gap-1">
            <Cpu className="h-3.5 w-3.5 text-zinc-400" /> Hypervisors
          </span>
          <span className="font-semibold text-zinc-200">
            {cluster.hostCount} Hosts
          </span>
        </div>

        <div className="flex items-center justify-between">
          <span className="text-zinc-500 font-normal flex items-center gap-1">
            <Monitor className="h-3.5 w-3.5 text-zinc-400" /> Workloads
          </span>
          <span className="font-semibold text-zinc-100">
            {cluster.vmCount} Virtual Machines
          </span>
        </div>
      </CardContent>

      <CardFooter className="p-4 pt-2.5 border-t border-zinc-800/60 flex items-center justify-between text-xs">
        <div className="flex items-center gap-2 font-mono text-[11px]">
          <span className={cluster.haEnabled ? "text-emerald-400" : "text-zinc-500"}>
            HA {cluster.haEnabled ? "ON" : "OFF"}
          </span>
          <span className="text-zinc-600">•</span>
          <span className={cluster.drsEnabled ? "text-emerald-400" : "text-zinc-500"}>
            DRS {cluster.drsEnabled ? "ON" : "OFF"}
          </span>
        </div>

        <div className="inline-flex items-center gap-1.5 px-2 py-0.5 rounded-none text-emerald-400 text-xs font-mono font-medium bg-emerald-400/10 border border-emerald-500/30">
          <span className="w-1.5 h-1.5 bg-emerald-400" />
          <span className="capitalize">{cluster.status}</span>
        </div>
      </CardFooter>
    </Card>
  );
};
