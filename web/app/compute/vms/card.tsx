import React from "react";
import { Monitor, Power } from "lucide-react";
import {
  Card,
  CardHeader,
  CardTitle,
  CardContent,
  CardFooter,
} from "@/components/ui/card";
import { type VirtualMachine } from "./columns";

interface VmCardProps {
  vm: VirtualMachine;
}

export const VmCard: React.FC<VmCardProps> = ({ vm }) => {
  const isOn = vm.powerState === "powered_on";

  return (
    <Card className="bg-[#0a0a0a] border border-zinc-800/80 text-[#e6e4e0] rounded-2xl p-0 overflow-hidden font-sans shadow-lg">
      <CardHeader className="p-5 pb-4 border-b border-zinc-800/80 flex-row items-center justify-between space-y-0">
        <CardTitle className="text-base font-bold tracking-tight text-white flex items-center gap-2 truncate">
          <Monitor className="h-4 w-4 text-emerald-400 shrink-0" />
          <span className="truncate">{vm.name}</span>
        </CardTitle>
        <div className="flex items-center gap-1.5 shrink-0">
          <span
            className={`inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-[11px] font-medium ${
              isOn ? "text-emerald-400 bg-emerald-400/10" : "text-zinc-500 bg-zinc-850"
            }`}
          >
            <Power className="h-2.5 w-2.5" />
            {isOn ? "On" : "Off"}
          </span>
        </div>
      </CardHeader>

      <CardContent className="p-5 space-y-3 text-sm">
        <div className="flex items-center justify-between">
          <span className="text-zinc-500 font-normal">IP Address</span>
          <span className="font-mono font-medium text-zinc-100">{vm.ip}</span>
        </div>

        <div className="flex items-center justify-between">
          <span className="text-zinc-500 font-normal">Operating System</span>
          <span className="font-medium text-zinc-300 text-xs truncate max-w-[200px]">
            {vm.os}
          </span>
        </div>

        <div className="flex items-center justify-between">
          <span className="text-zinc-500 font-normal">Location</span>
          <span className="font-semibold text-zinc-200">
            {vm.clusterId} • {vm.datacenterId}
          </span>
        </div>

        <div className="flex items-center justify-between">
          <span className="text-zinc-500 font-normal">Resources</span>
          <span className="font-semibold text-zinc-200">
            {vm.vcpus} vCPU • {vm.memoryGB} GB RAM
          </span>
        </div>

        <div className="flex items-center justify-between">
          <span className="text-zinc-500 font-normal">Utilization</span>
          <span className="font-semibold text-zinc-200">
            CPU: {vm.cpuUsagePercent}% • RAM: {vm.memoryUsagePercent}%
          </span>
        </div>
      </CardContent>

      <CardFooter className="p-5 pt-3 border-t border-zinc-800/60 flex items-center justify-between text-xs">
        <span className="text-zinc-500">Uptime: {vm.uptimeDays} days</span>
        <div className="inline-flex items-center gap-2 px-2.5 py-1 rounded-full text-emerald-400 text-xs font-medium bg-emerald-400/10">
          <span className="w-1.5 h-1.5 rounded-full bg-emerald-400" />
          <span className="capitalize">{vm.status}</span>
        </div>
      </CardFooter>
    </Card>
  );
};
