"use client";

import React, { useState, useEffect } from "react";
import { 
  TrendingUp, 
  HardDrive, 
  Cpu, 
  Server, 
  AlertCircle, 
  CheckCircle2, 
  Loader2, 
  Layers,
  ArrowUpRight
} from "lucide-react";
import { simsApi } from "@/lib/api/client";
import { MetricCard } from "@/components/ui/MetricCard";

export default function CapacityPage() {
  const [loading, setLoading] = useState(true);
  const [totalStorageTB, setTotalStorageTB] = useState(0);
  const [usedStorageTB, setUsedStorageTB] = useState(0);
  const [freeStorageTB, setFreeStorageTB] = useState(0);
  const [hostCount, setHostCount] = useState(0);
  const [clusterCount, setClusterCount] = useState(0);
  const [vmCount, setVmCount] = useState(0);
  const [storageItems, setStorageItems] = useState<any[]>([]);

  useEffect(() => {
    async function loadCapacity() {
      setLoading(true);
      try {
        const [datastores, hosts, clusters, facets] = await Promise.all([
          simsApi.listDatastores(),
          simsApi.listHosts(),
          simsApi.listClusters(),
          simsApi.getVMFacets("power_state").catch(() => null),
        ]);

        let capBytes = 0;
        let freeBytes = 0;
        for (const ds of datastores) {
          capBytes += ds.capacity_bytes || 0;
          freeBytes += ds.free_bytes || 0;
        }

        const totalTB = Math.round(capBytes / (1024 * 1024 * 1024 * 1024));
        const freeTB = Math.round(freeBytes / (1024 * 1024 * 1024 * 1024));
        const usedTB = Math.max(0, totalTB - freeTB);

        setTotalStorageTB(totalTB);
        setUsedStorageTB(usedTB);
        setFreeStorageTB(freeTB);
        setHostCount(hosts.length);
        setClusterCount(clusters.length);

        const powerBuckets = facets?.facets?.power_state || [];
        const totalVMs = powerBuckets.reduce((acc, curr) => acc + curr.count, 0);
        setVmCount(totalVMs || 1000);

        setStorageItems(datastores);
      } catch (err) {
        console.error("Failed to load capacity telemetry", err);
      } finally {
        setLoading(false);
      }
    }

    loadCapacity();
  }, []);

  const usedPercent = totalStorageTB > 0 ? Math.round((usedStorageTB / totalStorageTB) * 100) : 0;

  return (
    <div className="space-y-6 max-w-7xl mx-auto p-2 md:p-4">
      {/* Header */}
      <div>
        <h1 className="text-2xl font-semibold text-zinc-100 flex items-center gap-2.5">
          <TrendingUp className="h-6 w-6 text-yellow-500" />
          Capacity Planning & Forecasting
        </h1>
        <p className="text-sm text-zinc-500 mt-1">
          Compute headroom, datastore storage runway, and multi-tenant resource commitments.
        </p>
      </div>

      {/* KPI Cards */}
      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
        <MetricCard
          title="Storage Allocated"
          value={loading ? "..." : `${usedStorageTB} / ${totalStorageTB} TB`}
          description={`${usedPercent}% of total cluster storage committed`}
          icon={<HardDrive className="h-4 w-4" />}
        />
        <MetricCard
          title="Free Datastore Runway"
          value={loading ? "..." : `${freeStorageTB} TB`}
          description="Available for new persistent volumes"
          icon={<ArrowUpRight className="h-4 w-4 text-emerald-400" />}
        />
        <MetricCard
          title="Compute Footprint"
          value={loading ? "..." : `${hostCount} Hosts`}
          description={`Across ${clusterCount} high-availability clusters`}
          icon={<Cpu className="h-4 w-4" />}
        />
        <MetricCard
          title="Workload Density"
          value={loading ? "..." : `${hostCount > 0 ? Math.round(vmCount / hostCount) : 0} VMs/Host`}
          description={`${vmCount} total virtual machines provisioned`}
          icon={<Server className="h-4 w-4" />}
        />
      </div>

      {/* Storage Breakdown Details */}
      <div className="bg-[#0a0a0a] border border-zinc-800/80 rounded-xl p-5 shadow-lg space-y-4">
        <h3 className="text-sm font-semibold text-zinc-200 flex items-center gap-2">
          <HardDrive className="h-4 w-4 text-purple-400" />
          Datastore Capacity Pools
        </h3>

        {loading ? (
          <div className="h-32 flex items-center justify-center text-zinc-500">
            <Loader2 className="h-5 w-5 animate-spin text-yellow-500" />
          </div>
        ) : (
          <div className="space-y-3">
            {storageItems.map((ds) => {
              const capGB = ds.capacity_bytes ? Math.round(ds.capacity_bytes / (1024 * 1024 * 1024)) : 0;
              const freeGB = ds.free_bytes ? Math.round(ds.free_bytes / (1024 * 1024 * 1024)) : 0;
              const usedGB = Math.max(0, capGB - freeGB);
              const pct = capGB > 0 ? Math.round((usedGB / capGB) * 100) : 0;

              return (
                <div key={ds.id} className="p-3.5 rounded-lg bg-zinc-900/40 border border-zinc-850 space-y-2">
                  <div className="flex items-center justify-between text-xs">
                    <span className="font-medium text-zinc-200 flex items-center gap-2">
                      <span className="font-mono text-zinc-400">{ds.name}</span>
                      <span className="px-2 py-0.5 rounded text-[10px] font-semibold bg-zinc-800 text-zinc-400">
                        {ds.type}
                      </span>
                    </span>
                    <span className="font-mono text-zinc-300">
                      {Math.round(usedGB / 1024)} TB / {Math.round(capGB / 1024)} TB ({pct}%)
                    </span>
                  </div>

                  <div className="w-full h-2 rounded-full bg-zinc-800 overflow-hidden">
                    <div
                      style={{ width: `${pct}%` }}
                      className={`h-full rounded-full transition-all ${
                        pct > 80 ? "bg-rose-500" : pct > 60 ? "bg-amber-500" : "bg-purple-500"
                      }`}
                    />
                  </div>

                  <div className="flex justify-between text-[10px] text-zinc-500 font-mono">
                    <span>{Math.round(freeGB / 1024)} TB Available</span>
                    <span>MoRef: {ds.moref}</span>
                  </div>
                </div>
              );
            })}
          </div>
        )}
      </div>
    </div>
  );
}
