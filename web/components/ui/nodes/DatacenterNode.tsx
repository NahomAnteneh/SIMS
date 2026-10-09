import React, { memo } from "react";
import { Handle, Position, NodeProps } from "@xyflow/react";
import { Server, Layers, Cpu, CheckCircle2, AlertTriangle, XCircle } from "lucide-react";

// Types for node data
export interface DatacenterNodeData {
  label: string;
  sublabel?: string;
  type: "datacenter" | "cluster" | "host" | "vm";
  status?: "healthy" | "warning" | "critical";
  metrics?: {
    hosts?: number;
    vms?: number;
    cpu?: string;
    ram?: string;
  };
}

export const CustomInfraNode = memo(({ data, selected }: NodeProps<any>) => {
  const isDatacenter = data.type === "datacenter";
  const isCluster = data.type === "cluster";
  const isHealthy = data.status === "healthy";
  const isWarning = data.status === "warning";

  // Dynamic icon selection
  const Icon = isDatacenter ? Server : isCluster ? Layers : Cpu;

  return (
    <div
      className={`group relative bg-[#1c1b1a] text-[#e6e4e0] rounded-none p-4 border transition-all duration-200 min-w-[220px] shadow-xl ${
        selected ? "border-amber-500/80 ring-2 ring-amber-500/20" : "border-zinc-800 hover:border-zinc-700"
      }`}
    >
      {/* Target/Source Connection Handles */}
      <Handle
        type="target"
        position={Position.Top}
        className="!bg-zinc-600 !w-3 !h-3 !border-2 !border-[#1c1b1a]"
      />

      {/* Header Row */}
      <div className="flex items-center justify-between gap-3 pb-2 border-b border-zinc-800/80">
        <div className="flex items-center gap-2">
          <div className="p-1.5 rounded-none bg-zinc-900 border border-zinc-800 text-zinc-300">
            <Icon className="h-4 w-4" />
          </div>
          <div>
            <div className="text-xs font-semibold uppercase tracking-wider text-zinc-500">
              {data.type}
            </div>
            <div className="text-sm font-bold text-white tracking-tight">
              {data.label}
            </div>
          </div>
        </div>

        {/* Status Dot */}
        {data.status && (
          <div
            className={`w-2.5 h-2.5 rounded-none ${
              isHealthy
                ? "bg-emerald-400 shadow-[0_0_8px_rgba(52,211,153,0.5)]"
                : isWarning
                ? "bg-amber-400 shadow-[0_0_8px_rgba(251,191,36,0.5)]"
                : "bg-rose-500 shadow-[0_0_8px_rgba(244,63,94,0.5)]"
            }`}
          />
        )}
      </div>

      {/* Card Details / Metrics */}
      <div className="mt-3 space-y-1.5 text-xs text-zinc-400">
        {data.sublabel && (
          <div className="text-zinc-300 font-medium">{data.sublabel}</div>
        )}

        {data.metrics?.hosts !== undefined && (
          <div className="flex justify-between">
            <span className="text-zinc-500">Hosts</span>
            <span className="font-semibold text-zinc-200">{data.metrics.hosts}</span>
          </div>
        )}

        {data.metrics?.vms !== undefined && (
          <div className="flex justify-between">
            <span className="text-zinc-500">VMs</span>
            <span className="font-semibold text-zinc-200">{data.metrics.vms}</span>
          </div>
        )}

        {data.metrics?.cpu && (
          <div className="flex justify-between">
            <span className="text-zinc-500">CPU</span>
            <span className="font-semibold text-zinc-200">{data.metrics.cpu}</span>
          </div>
        )}
      </div>

      <Handle
        type="source"
        position={Position.Bottom}
        className="!bg-zinc-600 !w-3 !h-3 !border-2 !border-[#1c1b1a]"
      />
    </div>
  );
});

CustomInfraNode.displayName = "CustomInfraNode";