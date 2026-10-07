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
      <CardHeader className="p-5 pb-4 border-b border-zinc-800/80 flex-row items-center justify-between space-y-0">
        <CardTitle className="text-lg font-bold tracking-tight text-white flex items-center gap-2">
          <Network className="h-4 w-4 text-indigo-400" />
          {network.name}
        </CardTitle>
        <span className="text-xs text-zinc-500 font-mono">#{network.id}</span>
      </CardHeader>

      <CardContent className="p-5 space-y-3.5 text-sm">
        <div className="flex items-center justify-between">
          <span className="text-zinc-500 font-normal">Datacenter</span>
          <span className="font-semibold text-zinc-200">
            {network.datacenterId}
          </span>
        </div>

        <div className="flex items-center justify-between">
          <span className="text-zinc-500 font-normal">Type & VLAN ID</span>
          <span className="font-semibold text-zinc-200">
            {network.type} (VLAN {network.vlanId})
          </span>
        </div>

        <div className="flex items-center justify-between">
          <span className="text-zinc-500 font-normal">Subnet CIDR</span>
          <span className="font-mono font-medium text-zinc-100">
            {network.subnet}
          </span>
        </div>
      </CardContent>

      <CardFooter className="p-5 pt-3 border-t border-zinc-800/60 flex items-center justify-between text-xs">
        <div className="flex items-center gap-1.5 text-zinc-400">
          <Tag className="h-3.5 w-3.5 text-zinc-400" />
          <span>Standard Port Group</span>
        </div>

        <div
          className={`inline-flex items-center gap-2 px-2.5 py-1 rounded-full text-xs font-medium ${
            isActive
              ? "text-emerald-400 bg-emerald-400/10"
              : "text-zinc-400 bg-zinc-800"
          }`}
        >
          <span
            className={`w-1.5 h-1.5 rounded-full ${
              isActive ? "bg-emerald-400" : "bg-zinc-500"
            }`}
          />
          <span className="capitalize">{network.status}</span>
        </div>
      </CardFooter>
    </Card>
  );
};
