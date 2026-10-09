"use client";

import React from "react";
import { Monitor, Power, Clock, FolderTree } from "lucide-react";
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
  onClick?: () => void;
}

export const VmCard: React.FC<VmCardProps> = ({ vm, onClick }) => {
  const isOn = vm.power_state === "poweredOn";
  const isSuspended = vm.power_state === "suspended";

  return (
    <Card
      onClick={onClick}
      className={`bg-[#0c0c0c] border border-zinc-800/80 text-[#e6e4e0] rounded-none p-0 overflow-hidden font-sans shadow-lg transition-all ${
        onClick ? "cursor-pointer hover:border-zinc-700 hover:bg-zinc-900/40" : ""
      }`}
    >
      <CardHeader className="p-4 pb-3 border-b border-zinc-800/80 flex-row items-center justify-between space-y-0">
        <CardTitle className="text-sm font-bold tracking-tight text-white flex items-center gap-2 truncate">
          <Monitor className="h-4 w-4 text-yellow-600 shrink-0" />
          <span className="truncate">{vm.name}</span>
        </CardTitle>
        <div className="flex items-center gap-1.5 shrink-0">
          <span
            className={`inline-flex items-center gap-1 px-1.5 py-0.2 rounded-none text-[10px] font-mono uppercase font-bold border ${
              isOn
                ? "text-emerald-400 bg-emerald-400/10 border-emerald-500/30"
                : isSuspended
                ? "text-amber-400 bg-amber-400/10 border-amber-500/30"
                : "text-zinc-500 bg-zinc-800 border-zinc-700"
            }`}
          >
            <Power className="h-2.5 w-2.5" />
            {isOn ? "On" : isSuspended ? "Suspended" : "Off"}
          </span>
        </div>
      </CardHeader>

      <CardContent className="p-4 space-y-2.5 text-xs">
        <div className="flex items-center justify-between">
          <span className="text-zinc-500 font-normal">IP Address</span>
          <span className="font-mono font-medium text-zinc-200">
            {vm.guest_ip_address || "None"}
          </span>
        </div>

        <div className="flex items-center justify-between">
          <span className="text-zinc-500 font-normal">Operating System</span>
          <span className="font-medium text-zinc-300 truncate max-w-[180px]">
            {vm.guest_os || vm.moref}
          </span>
        </div>

        <div className="flex items-center justify-between">
          <span className="text-zinc-500 font-normal">Folder</span>
          <span className="font-mono text-zinc-300 truncate max-w-[180px] flex items-center gap-1">
            <FolderTree className="h-3 w-3 text-yellow-600" />
            {vm.folder_path || "Root"}
          </span>
        </div>

        <div className="flex items-center justify-between">
          <span className="text-zinc-500 font-normal">Resources</span>
          <span className="font-medium text-zinc-200">
            {vm.cpu_cores} vCPU • {(vm.memory_mb / 1024).toFixed(1)} GB RAM
          </span>
        </div>
      </CardContent>

      <CardFooter className="p-4 pt-2.5 border-t border-zinc-800/60 flex items-center justify-between text-[11px] text-zinc-500">
        <span className="flex items-center gap-1">
          <Clock className="h-3 w-3" />
          {vm.created_at ? new Date(vm.created_at).toLocaleDateString() : "N/A"}
        </span>
        <div className="inline-flex items-center gap-1 text-zinc-400 font-mono">
          {vm.moref}
        </div>
      </CardFooter>
    </Card>
  );
};
