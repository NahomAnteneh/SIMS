import React from "react";
import { Layers, ShieldCheck } from "lucide-react";
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
      <CardHeader className="p-5 pb-4 border-b border-zinc-800/80 flex-row items-center justify-between space-y-0">
        <CardTitle className="text-lg font-bold tracking-tight text-white flex items-center gap-2">
          <Layers className="h-4 w-4 text-amber-500" />
          {cluster.name}
        </CardTitle>
        <span className="text-xs text-zinc-500 font-mono">#{cluster.id}</span>
      </CardHeader>

      <CardContent className="p-5 space-y-3.5 text-sm">
        <div className="flex items-center justify-between">
          <span className="text-zinc-500 font-normal">Datacenter</span>
          <span className="font-semibold text-zinc-200">
            {cluster.datacenterId}
          </span>
        </div>

        <div className="flex items-center justify-between">
          <span className="text-zinc-500 font-normal">vCenter Server</span>
          <span className="font-medium text-zinc-300">
            {cluster.vcenterId}
          </span>
        </div>

        <div className="flex items-center justify-between">
          <span className="text-zinc-500 font-normal">Hosts & VMs</span>
          <span className="font-semibold text-zinc-200">
            {cluster.hostCount} Hosts • {cluster.vmCount} VMs
          </span>
        </div>

        <div className="flex items-center justify-between">
          <span className="text-zinc-500 font-normal">CPU Allocation</span>
          <span className="font-semibold text-zinc-200">
            {cluster.usedCpuGHz} / {cluster.totalCpuGHz} GHz
          </span>
        </div>

        <div className="flex items-center justify-between">
          <span className="text-zinc-500 font-normal">Memory Allocation</span>
          <span className="font-semibold text-zinc-200">
            {cluster.usedMemoryGB} / {cluster.totalMemoryGB} GB
          </span>
        </div>
      </CardContent>

      <CardFooter className="p-5 pt-3 border-t border-zinc-800/60 flex items-center justify-between text-xs">
        <div className="flex items-center gap-2 text-zinc-400">
          <ShieldCheck className="h-3.5 w-3.5 text-zinc-400" />
          <span className={cluster.haEnabled ? "text-emerald-400 font-medium" : "text-zinc-500"}>
            HA {cluster.haEnabled ? "ON" : "OFF"}
          </span>
          <span>•</span>
          <span className={cluster.drsEnabled ? "text-emerald-400 font-medium" : "text-zinc-500"}>
            DRS {cluster.drsEnabled ? "ON" : "OFF"}
          </span>
        </div>

        <div className="inline-flex items-center gap-2 px-2 py-0.5 rounded-none text-emerald-400 text-xs font-mono font-medium bg-emerald-400/10 border border-emerald-500/30">
          <span className="w-1.5 h-1.5 rounded-none bg-emerald-400" />
          <span className="capitalize">{cluster.status}</span>
        </div>
      </CardFooter>
    </Card>
  );
};
