import React from "react";
import { HardDrive, Zap, Monitor } from "lucide-react";
import {
  Card,
  CardHeader,
  CardTitle,
  CardContent,
  CardFooter,
} from "@/components/ui/card";
import { type Storage } from "../columns";

interface StorageCardProps {
  storage: Storage;
  onClick?: () => void;
}

export const StorageCard: React.FC<StorageCardProps> = ({ storage, onClick }) => {
  const hasCapacity = storage.capacityGB > 0;
  const percent = hasCapacity ? Math.round((storage.usedGB / storage.capacityGB) * 100) : 0;
  const usedTB = (storage.usedGB / 1024).toFixed(1);
  const totalTB = (storage.capacityGB / 1024).toFixed(1);
  const freeTB = (storage.freeGB / 1024).toFixed(1);

  return (
    <Card
      onClick={onClick}
      className={`bg-[#0a0a0a] border border-zinc-800/80 text-[#e6e4e0] rounded-none p-0 overflow-hidden font-sans shadow-lg transition-all ${
        onClick ? "hover:border-yellow-600/50 hover:bg-zinc-900/30 cursor-pointer" : ""
      }`}
    >
      <CardHeader className="p-4 pb-3 border-b border-zinc-800/80 flex-row items-center justify-between space-y-0">
        <CardTitle className="text-base font-bold tracking-tight text-white flex items-center gap-2">
          <HardDrive className="h-4 w-4 text-cyan-500" />
          {storage.name || "Unnamed Datastore"}
        </CardTitle>
        <span className="text-xs text-zinc-500 font-mono">
          #{storage.moref || storage.id.slice(0, 8)}
        </span>
      </CardHeader>

      <CardContent className="p-4 space-y-3 text-sm">
        <div className="flex items-center justify-between">
          <span className="text-zinc-500 font-normal">Datacenter</span>
          <span className="font-semibold text-zinc-200">
            {storage.datacenterName || "Datacenter"}
          </span>
        </div>

        <div className="flex items-center justify-between">
          <span className="text-zinc-500 font-normal">vCenter</span>
          <span className="font-medium text-zinc-300 text-xs truncate max-w-[200px]">
            {storage.vcenterName || storage.vcenterId}
          </span>
        </div>

        <div className="flex items-center justify-between">
          <span className="text-zinc-500 font-normal">Type / Format</span>
          <span className="font-semibold text-zinc-200 font-mono text-xs">
            {storage.type || "VMFS"}
          </span>
        </div>

        <div className="flex items-center justify-between">
          <span className="text-zinc-500 font-normal">Capacity Allocation</span>
          <span className="font-semibold text-zinc-200 font-mono text-xs">
            {hasCapacity ? `${usedTB} TB / ${totalTB} TB (${percent}%)` : "Unreported / Dynamic"}
          </span>
        </div>

        <div className="flex items-center justify-between">
          <span className="text-zinc-500 font-normal">Attached Workloads</span>
          <span className="font-semibold text-zinc-100 font-mono text-xs flex items-center gap-1.5">
            <Monitor className="h-3.5 w-3.5 text-yellow-500" />
            {storage.vmCount || 0} VMs
          </span>
        </div>

        {/* Storage Bar */}
        {hasCapacity && (
          <div className="space-y-1">
            <div className="h-1.5 w-full bg-zinc-800 rounded-none overflow-hidden">
              <div
                className={`h-full ${
                  percent > 85 ? "bg-rose-500" : percent > 70 ? "bg-amber-500" : "bg-cyan-500"
                }`}
                style={{ width: `${Math.min(percent, 100)}%` }}
              />
            </div>
            <div className="flex justify-between text-xs text-zinc-500 font-mono">
              <span>{freeTB} TB Free</span>
              <span>{percent}% Used</span>
            </div>
          </div>
        )}
      </CardContent>

      <CardFooter className="p-4 pt-2.5 border-t border-zinc-800/60 flex items-center justify-between text-xs">
        <div className="flex items-center gap-1.5 text-zinc-400 font-mono text-[11px]">
          <Zap className="h-3.5 w-3.5 text-yellow-500" />
          <span>Shared Storage Pool</span>
        </div>

        <div className="inline-flex items-center gap-1.5 px-2 py-0.5 rounded-none text-emerald-400 text-xs font-mono font-medium bg-emerald-400/10 border border-emerald-500/30">
          <span className="w-1.5 h-1.5 bg-emerald-400" />
          <span className="capitalize">{storage.status}</span>
        </div>
      </CardFooter>
    </Card>
  );
};
