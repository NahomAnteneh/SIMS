import React from "react";
import { Network, Tag } from "lucide-react";
import {
  Card,
  CardHeader,
  CardTitle,
  CardContent,
  CardFooter,
} from "@/components/ui/card";
import { type NetworkItem } from "../columns";

interface NetworkCardProps {
  network: NetworkItem;
  onClick?: () => void;
}

export const NetworkCard: React.FC<NetworkCardProps> = ({ network, onClick }) => {
  const isActive = network.status === "active";

  return (
    <Card
      onClick={onClick}
      className={`bg-[#0a0a0a] border border-zinc-800/80 text-[#e6e4e0] rounded-none p-0 overflow-hidden font-sans shadow-lg transition-all ${
        onClick ? "hover:border-yellow-600/50 hover:bg-zinc-900/30 cursor-pointer" : ""
      }`}
    >
      <CardHeader className="p-4 pb-3 border-b border-zinc-800/80 flex-row items-center justify-between space-y-0">
        <CardTitle className="text-base font-bold tracking-tight text-white flex items-center gap-2">
          <Network className="h-4 w-4 text-emerald-400" />
          {network.name || "Default Network"}
        </CardTitle>
        <span className="text-xs text-zinc-500 font-mono">
          #{network.moref || network.id.slice(0, 8)}
        </span>
      </CardHeader>

      <CardContent className="p-4 space-y-3 text-sm">
        <div className="flex items-center justify-between">
          <span className="text-zinc-500 font-normal">Datacenter</span>
          <span className="font-semibold text-zinc-200">
            {network.datacenterName || "Primary Datacenter"}
          </span>
        </div>

        <div className="flex items-center justify-between">
          <span className="text-zinc-500 font-normal">vCenter</span>
          <span className="font-medium text-zinc-300 text-xs truncate max-w-[200px]">
            {network.vcenterName || network.vcenterId}
          </span>
        </div>

        <div className="flex items-center justify-between">
          <span className="text-zinc-500 font-normal">Network Type</span>
          <span className="font-semibold text-zinc-200 text-xs font-mono">
            {network.type || "Standard"}
          </span>
        </div>
      </CardContent>

      <CardFooter className="p-4 pt-2.5 border-t border-zinc-800/60 flex items-center justify-between text-xs">
        <div className="flex items-center gap-1.5 text-zinc-400 font-mono text-[11px]">
          <Tag className="h-3.5 w-3.5 text-zinc-400" />
          <span>vSphere Port Group</span>
        </div>

        <div
          className={`inline-flex items-center gap-1.5 px-2 py-0.5 rounded-none text-xs font-mono font-medium border ${
            isActive
              ? "text-emerald-400 bg-emerald-500/10 border-emerald-500/30"
              : "text-zinc-400 bg-zinc-800 border-zinc-700"
          }`}
        >
          <span
            className={`w-1.5 h-1.5 ${
              isActive ? "bg-emerald-400" : "bg-zinc-500"
            }`}
          />
          <span className="capitalize">{network.status}</span>
        </div>
      </CardFooter>
    </Card>
  );
};
