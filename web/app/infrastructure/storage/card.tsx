import React from "react";
import { HardDrive, Zap } from "lucide-react";
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
  const percent = Math.round((storage.usedGB / storage.capacityGB) * 100);
  const usedTB = (storage.usedGB / 1024).toFixed(1);
  const totalTB = (storage.capacityGB / 1024).toFixed(1);
  const freeTB = (storage.freeGB / 1024).toFixed(1);

  return (
    <Card
      onClick={onClick}
      className={`bg-[#0a0a0a] border border-zinc-800/80 text-[#e6e4e0] rounded-2xl p-0 overflow-hidden font-sans shadow-lg transition-all ${
        onClick ? "hover:border-yellow-600/50 hover:bg-zinc-900/30 cursor-pointer" : ""
      }`}
    >
      <CardHeader className="p-5 pb-4 border-b border-zinc-800/80 flex-row items-center justify-between space-y-0">
        <CardTitle className="text-lg font-bold tracking-tight text-white flex items-center gap-2">
          <HardDrive className="h-4 w-4 text-cyan-500" />
          {storage.name}
        </CardTitle>
        <span className="text-xs text-zinc-500 font-mono">#{storage.id}</span>
      </CardHeader>

      <CardContent className="p-5 space-y-3.5 text-sm">
        <div className="flex items-center justify-between">
          <span className="text-zinc-500 font-normal">Datacenter</span>
          <span className="font-semibold text-zinc-200">
            {storage.datacenterId}
          </span>
        </div>

        <div className="flex items-center justify-between">
          <span className="text-zinc-500 font-normal">Type / Format</span>
          <span className="font-semibold text-zinc-200">
            {storage.type}
          </span>
        </div>

        <div className="flex items-center justify-between">
          <span className="text-zinc-500 font-normal">Capacity Allocation</span>
          <span className="font-semibold text-zinc-200">
            {usedTB} TB / {totalTB} TB ({percent}%)
          </span>
        </div>

        {/* Storage Bar */}
        <div className="space-y-1">
          <div className="h-1.5 w-full bg-zinc-800 rounded-full overflow-hidden">
            <div
              className={`h-full ${
                percent > 85 ? "bg-rose-500" : percent > 70 ? "bg-amber-500" : "bg-cyan-500"
              }`}
              style={{ width: `${Math.min(percent, 100)}%` }}
            />
          </div>
          <div className="flex justify-between text-xs text-zinc-500">
            <span>{freeTB} TB Free</span>
            <span>{percent}% Used</span>
          </div>
        </div>
      </CardContent>

      <CardFooter className="p-5 pt-3 border-t border-zinc-800/60 flex items-center justify-between text-xs">
        <div className="flex items-center gap-1.5 text-zinc-400">
          <Zap className="h-3.5 w-3.5 text-zinc-400" />
          <span>Shared Storage Pool</span>
        </div>

        <div className="inline-flex items-center gap-2 px-2.5 py-1 rounded-full text-emerald-400 text-xs font-medium bg-emerald-400/10">
          <span className="w-1.5 h-1.5 rounded-full bg-emerald-400" />
          <span className="capitalize">{storage.status}</span>
        </div>
      </CardFooter>
    </Card>
  );
};
