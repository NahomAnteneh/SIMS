import React from "react";
import { Server } from "lucide-react";
import {
  Card,
  CardHeader,
  CardTitle,
  CardContent,
  CardFooter,
} from "@/components/ui/card";

export interface DatacenterCardData {
  id: string;
  name: string;
  displayName: string;
  location: string;
  country: string;
  siteCode: string;
  environment: string;
  status: "healthy" | "warning" | "critical" | string;
  vcenterCount: number;
  clusterCount: number;
  hostCount: number;
  vmCount: number;
  applicationCount: number;
  clientCount: number;
  totalCpuGHz: number;
  usedCpuGHz: number;
  totalMemoryGB: number;
  usedMemoryGB: number;
  totalStorageTB: number;
  usedStorageTB: number;
  lastSync?: string;
}

interface DatacenterCardProps {
  datacenter?: DatacenterCardData;
  onClick?: () => void;
}

const sampleData: DatacenterCardData = {
  id: "dc-001",
  name: "DC-Addis-01",
  displayName: "Addis Ababa Primary Data Center",
  location: "Addis Ababa, Ethiopia",
  country: "Ethiopia",
  siteCode: "ADD01",
  environment: "production",
  status: "healthy",
  vcenterCount: 2,
  clusterCount: 4,
  hostCount: 12,
  vmCount: 96,
  applicationCount: 18,
  clientCount: 8,
  totalCpuGHz: 1536,
  usedCpuGHz: 712,
  totalMemoryGB: 6144,
  usedMemoryGB: 3980,
  totalStorageTB: 240,
  usedStorageTB: 156,
  lastSync: "10 SEP, 2026",
};

export const DatacenterCard: React.FC<DatacenterCardProps> = ({
  datacenter = sampleData,
  onClick,
}) => {
  return (
    <Card
      onClick={onClick}
      className={`bg-[#0a0a0a] border border-zinc-800/80 text-[#e6e4e0] rounded-none p-0 overflow-hidden font-sans shadow-lg max-w-sm transition-all ${
        onClick ? "hover:border-yellow-600/50 hover:bg-zinc-900/30 cursor-pointer" : ""
      }`}
    >
      {/* Top Header Row */}
      <CardHeader className="p-5 pb-4 border-b border-zinc-800/80 flex-row items-center justify-between space-y-0">
        <CardTitle className="text-lg font-bold tracking-tight text-white">
          #{datacenter.siteCode}
        </CardTitle>

        {/* Minimal Connection / Server Marker */}
        <div className="flex items-center gap-2 text-xs font-semibold text-zinc-400">
          <div className="flex items-center gap-1.5">
            <span className="w-2 h-2 rounded-none border border-zinc-500" />
            <div className="w-7 h-[1px] bg-zinc-700" />
            <span className="w-2 h-2 rounded-none bg-zinc-100" />
          </div>
          <Server className="h-4 w-4 ml-1 text-zinc-300" />
          <span>{datacenter.hostCount} Hosts</span>
        </div>
      </CardHeader>

      {/* Main Plain-Text Key-Value Rows */}
      <CardContent className="p-5 space-y-4 text-sm">
        {/* Datacenter Row */}
        <div className="flex items-center justify-between">
          <span className="text-zinc-500 font-normal">Datacenter</span>
          <span className="font-semibold text-zinc-200 text-base">
            {datacenter.displayName}
          </span>
        </div>

        {/* Location Row */}
        <div className="flex items-center justify-between">
          <span className="text-zinc-500 font-normal">Location</span>
          <span className="font-medium text-zinc-300">
            {datacenter.location}
          </span>
        </div>

        {/* Capacity / Footprint Row */}
        <div className="flex items-center justify-between">
          <span className="text-zinc-500 font-normal">Workloads</span>
          <span className="font-semibold text-zinc-200 text-base">
            {datacenter.vmCount} VMs, {datacenter.applicationCount} Apps
          </span>
        </div>

        {/* Resource Allocation Row */}
        <div className="flex items-center justify-between">
          <span className="text-zinc-500 font-normal">Storage</span>
          <span className="font-semibold text-zinc-200 text-base">
            {datacenter.usedStorageTB} / {datacenter.totalStorageTB} TB
          </span>
        </div>
      </CardContent>

      {/* Footer Row */}
      <CardFooter className="p-5 pt-2 flex items-center justify-between text-xs">
        <div className="text-zinc-400 font-medium tracking-wide">
          SYNC - <span className="text-zinc-200 font-semibold">{datacenter.lastSync}</span>
        </div>

        {/* Subtle Dark Pill Status */}
        <div className="inline-flex items-center gap-2 px-2 py-0.5 rounded-none text-emerald-400 text-xs font-mono font-medium bg-emerald-400/10 border border-emerald-500/30">
          <span className="w-2 h-2 rounded-none bg-emerald-400" />
          <span className="capitalize">{datacenter.status}</span>
        </div>
      </CardFooter>
    </Card>
  );
};