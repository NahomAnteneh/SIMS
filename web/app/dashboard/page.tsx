"use client";

import React, { useState, useEffect } from "react";
import { MetricCard } from "@/components/ui/MetricCard";
import { 
  Server, 
  Building2, 
  Cpu, 
  Layers, 
  HardDrive, 
  Network,
  Power,
  HardDriveDownload,
  Activity,
  Layers2
} from "lucide-react";
import { simsApi } from "@/lib/api/client";

interface PowerDistribution {
  poweredOn: number;
  poweredOff: number;
  suspended: number;
  total: number;
}

interface ItemBucket {
  name: string;
  count: number;
  percentage: number;
}

export default function ReportsDashboard() {
  const [vmCount, setVmCount] = useState<number>(0);
  const [dcCount, setDcCount] = useState<number>(0);
  const [vcCount, setVcCount] = useState<number>(0);
  const [clusterCount, setClusterCount] = useState<number>(0);
  const [hostCount, setHostCount] = useState<number>(0);
  const [datastoreCount, setDatastoreCount] = useState<number>(0);
  const [networkCount, setNetworkCount] = useState<number>(0);
  
  const [powerDist, setPowerDist] = useState<PowerDistribution>({
    poweredOn: 0,
    poweredOff: 0,
    suspended: 0,
    total: 0,
  });

  const [osBuckets, setOsBuckets] = useState<ItemBucket[]>([]);
  const [dcBuckets, setDcBuckets] = useState<ItemBucket[]>([]);
  const [storageStats, setStorageStats] = useState({
    totalTB: 0,
    usedTB: 0,
    freeTB: 0,
    usedPercent: 0,
  });

  const [loading, setLoading] = useState(true);

  useEffect(() => {
    async function loadMetrics() {
      try {
        const [facetsRes, dcs, vcs, clusters, hosts, datastores, networks] = await Promise.allSettled([
          simsApi.getVMFacets("power_state,datacenter,guest_os,cluster"),
          simsApi.listDatacenters(),
          simsApi.listVCenters(),
          simsApi.listClusters(),
          simsApi.listHosts(),
          simsApi.listDatastores(),
          simsApi.listNetworks(),
        ]);

        let totalVMs = 0;
        if (facetsRes.status === "fulfilled" && facetsRes.value.facets) {
          const powerBuckets = facetsRes.value.facets.power_state || [];
          let on = 0;
          let off = 0;
          let susp = 0;

          for (const b of powerBuckets) {
            if (b.value === "poweredOn") on = b.count;
            else if (b.value === "poweredOff") off = b.count;
            else if (b.value === "suspended") susp = b.count;
          }

          totalVMs = on + off + susp;
          setVmCount(totalVMs);
          setPowerDist({
            poweredOn: on,
            poweredOff: off,
            suspended: susp,
            total: totalVMs,
          });

          // OS distribution
          const rawOS = facetsRes.value.facets.guest_os || [];
          const osList = rawOS.map((b) => ({
            name: b.value.replace(/_64Guest|Guest/g, "").replace(/([a-z])([0-9])/i, "$1 $2"),
            count: b.count,
            percentage: totalVMs > 0 ? Math.round((b.count / totalVMs) * 100) : 0,
          }));
          setOsBuckets(osList);

          // DC distribution
          const rawDC = facetsRes.value.facets.datacenter || [];
          const dcList = rawDC.map((b) => ({
            name: b.value,
            count: b.count,
            percentage: totalVMs > 0 ? Math.round((b.count / totalVMs) * 100) : 0,
          }));
          setDcBuckets(dcList);
        }

        if (dcs.status === "fulfilled") setDcCount(dcs.value.length);
        if (vcs.status === "fulfilled") setVcCount(vcs.value.length);
        if (clusters.status === "fulfilled") setClusterCount(clusters.value.length);
        if (hosts.status === "fulfilled") setHostCount(hosts.value.length);
        if (networks.status === "fulfilled") setNetworkCount(networks.value.length);

        if (datastores.status === "fulfilled") {
          setDatastoreCount(datastores.value.length);
          let capBytes = 0;
          let freeBytes = 0;
          for (const ds of datastores.value) {
            capBytes += ds.capacity_bytes || 0;
            freeBytes += ds.free_bytes || 0;
          }
          const totalTB = Math.round(capBytes / (1024 * 1024 * 1024 * 1024));
          const freeTB = Math.round(freeBytes / (1024 * 1024 * 1024 * 1024));
          const usedTB = Math.max(0, totalTB - freeTB);
          const usedPercent = totalTB > 0 ? Math.round((usedTB / totalTB) * 100) : 0;
          setStorageStats({ totalTB, usedTB, freeTB, usedPercent });
        }
      } catch (err) {
        console.error("Failed to load dashboard metrics", err);
      } finally {
        setLoading(false);
      }
    }

    loadMetrics();
  }, []);

  return (
    <div className="max-w-7xl mx-auto space-y-6 p-4 md:p-6">
      {/* Page Header */}
      <div>
        <h1 className="text-2xl font-semibold text-zinc-100">Executive Overview</h1>
        <p className="text-sm text-zinc-500 mt-1">
          Real-time global inventory telemetry from SIMS Go Query API across connected vCenters
        </p>
      </div>

      {/* Responsive Infrastructure Metrics Grid (6 Columns) */}
      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-6 gap-4">
        <MetricCard 
          title="Virtual Machines" 
          value={loading ? "..." : vmCount.toLocaleString()}
          description={`${powerDist.poweredOn} running, ${powerDist.poweredOff} stopped`} 
          icon={<Server className="h-4 w-4" />} 
        />
        <MetricCard 
          title="ESXi Hosts" 
          value={loading ? "..." : `${hostCount}`}
          description={`Across ${clusterCount} clusters`} 
          icon={<Cpu className="h-4 w-4" />} 
        />
        <MetricCard 
          title="Datacenters" 
          value={loading ? "..." : `${dcCount}`}
          description="Physical availability zones" 
          icon={<Building2 className="h-4 w-4" />} 
        />
        <MetricCard 
          title="vCenters" 
          value={loading ? "..." : `${vcCount}`}
          description="Managed management endpoints" 
          icon={<Layers className="h-4 w-4" />} 
        />
        <MetricCard 
          title="Datastores" 
          value={loading ? "..." : `${datastoreCount}`}
          description={`${storageStats.totalTB} TB aggregate storage`} 
          icon={<HardDrive className="h-4 w-4" />} 
        />
        <MetricCard 
          title="Networks" 
          value={loading ? "..." : `${networkCount}`}
          description="Distributed portgroups" 
          icon={<Network className="h-4 w-4" />} 
        />
      </div>

      {/* Main Real Telemetry Grid */}
      <div className="grid grid-cols-1 lg:grid-cols-3 gap-6">
        
        {/* Power State Distribution */}
        <div className="bg-[#111111] border border-zinc-800/80 rounded-xl p-5 flex flex-col justify-between">
          <div>
            <div className="flex items-center justify-between mb-4">
              <h3 className="text-sm font-medium text-zinc-200 flex items-center gap-2">
                <Power className="h-4 w-4 text-emerald-500" />
                Workload Power States
              </h3>
              <span className="text-xs text-zinc-500 font-mono">
                {vmCount} total
              </span>
            </div>

            {/* Visual Progress Bar */}
            <div className="w-full h-3 rounded-full bg-zinc-800 overflow-hidden flex mb-6">
              <div 
                style={{ width: `${vmCount > 0 ? (powerDist.poweredOn / vmCount) * 100 : 0}%` }}
                className="bg-emerald-500 h-full transition-all"
                title={`Powered On: ${powerDist.poweredOn}`}
              />
              <div 
                style={{ width: `${vmCount > 0 ? (powerDist.suspended / vmCount) * 100 : 0}%` }}
                className="bg-amber-500 h-full transition-all"
                title={`Suspended: ${powerDist.suspended}`}
              />
              <div 
                style={{ width: `${vmCount > 0 ? (powerDist.poweredOff / vmCount) * 100 : 0}%` }}
                className="bg-zinc-600 h-full transition-all"
                title={`Powered Off: ${powerDist.poweredOff}`}
              />
            </div>

            {/* Stats Breakdown */}
            <div className="space-y-3">
              <div className="flex items-center justify-between text-xs">
                <span className="flex items-center gap-2 text-zinc-300">
                  <span className="w-2.5 h-2.5 rounded-full bg-emerald-500"></span>
                  Powered On
                </span>
                <div className="flex items-center gap-2">
                  <span className="font-semibold text-zinc-100">{powerDist.poweredOn}</span>
                  <span className="text-zinc-500 font-mono">
                    ({vmCount > 0 ? Math.round((powerDist.poweredOn / vmCount) * 100) : 0}%)
                  </span>
                </div>
              </div>

              <div className="flex items-center justify-between text-xs">
                <span className="flex items-center gap-2 text-zinc-300">
                  <span className="w-2.5 h-2.5 rounded-full bg-amber-500"></span>
                  Suspended
                </span>
                <div className="flex items-center gap-2">
                  <span className="font-semibold text-zinc-100">{powerDist.suspended}</span>
                  <span className="text-zinc-500 font-mono">
                    ({vmCount > 0 ? Math.round((powerDist.suspended / vmCount) * 100) : 0}%)
                  </span>
                </div>
              </div>

              <div className="flex items-center justify-between text-xs">
                <span className="flex items-center gap-2 text-zinc-300">
                  <span className="w-2.5 h-2.5 rounded-full bg-zinc-600"></span>
                  Powered Off
                </span>
                <div className="flex items-center gap-2">
                  <span className="font-semibold text-zinc-100">{powerDist.poweredOff}</span>
                  <span className="text-zinc-500 font-mono">
                    ({vmCount > 0 ? Math.round((powerDist.poweredOff / vmCount) * 100) : 0}%)
                  </span>
                </div>
              </div>
            </div>
          </div>

          <div className="pt-4 border-t border-zinc-800/80 mt-6 text-[11px] text-zinc-500">
            Computed live from backend <code className="text-zinc-400">/v1/vms/facets</code>
          </div>
        </div>

        {/* Operating System Distribution */}
        <div className="bg-[#111111] border border-zinc-800/80 rounded-xl p-5 flex flex-col justify-between">
          <div>
            <div className="flex items-center justify-between mb-4">
              <h3 className="text-sm font-medium text-zinc-200 flex items-center gap-2">
                <Activity className="h-4 w-4 text-blue-500" />
                Guest Operating Systems
              </h3>
              <span className="text-xs text-zinc-500 font-mono">
                {osBuckets.length} platforms
              </span>
            </div>

            <div className="space-y-3.5">
              {osBuckets.map((os, idx) => (
                <div key={idx} className="space-y-1">
                  <div className="flex items-center justify-between text-xs">
                    <span className="text-zinc-300 capitalize truncate max-w-[160px]">{os.name}</span>
                    <span className="font-mono text-zinc-400">
                      {os.count} <span className="text-zinc-600">({os.percentage}%)</span>
                    </span>
                  </div>
                  <div className="w-full h-1.5 rounded-full bg-zinc-800 overflow-hidden">
                    <div 
                      style={{ width: `${os.percentage}%` }}
                      className="bg-blue-500 h-full rounded-full transition-all"
                    />
                  </div>
                </div>
              ))}
            </div>
          </div>

          <div className="pt-4 border-t border-zinc-800/80 mt-6 text-[11px] text-zinc-500">
            Detected via VMware Tools telemetry
          </div>
        </div>

        {/* Datacenter Workload Allocation & Storage */}
        <div className="bg-[#111111] border border-zinc-800/80 rounded-xl p-5 flex flex-col justify-between">
          <div>
            <div className="flex items-center justify-between mb-4">
              <h3 className="text-sm font-medium text-zinc-200 flex items-center gap-2">
                <Layers2 className="h-4 w-4 text-yellow-500" />
                Datacenter Allocation
              </h3>
              <span className="text-xs text-zinc-500 font-mono">
                {dcBuckets.length} facilities
              </span>
            </div>

            <div className="space-y-4 mb-6">
              {dcBuckets.map((dc, idx) => (
                <div key={idx} className="space-y-1.5">
                  <div className="flex items-center justify-between text-xs">
                    <span className="text-zinc-200 font-medium">{dc.name}</span>
                    <span className="font-mono text-zinc-300 font-semibold">
                      {dc.count} VMs <span className="text-zinc-500 font-normal">({dc.percentage}%)</span>
                    </span>
                  </div>
                  <div className="w-full h-2 rounded-full bg-zinc-800 overflow-hidden">
                    <div 
                      style={{ width: `${dc.percentage}%` }}
                      className="bg-yellow-600 h-full rounded-full transition-all"
                    />
                  </div>
                </div>
              ))}
            </div>

            {/* Storage capacity banner */}
            <div className="p-3 rounded-lg bg-zinc-900/60 border border-zinc-800/80 space-y-2">
              <div className="flex items-center justify-between text-xs">
                <span className="text-zinc-300 flex items-center gap-1.5">
                  <HardDriveDownload className="h-3.5 w-3.5 text-zinc-400" />
                  Aggregate Datastore Capacity
                </span>
                <span className="font-mono text-zinc-200 font-medium">
                  {storageStats.usedTB} / {storageStats.totalTB} TB
                </span>
              </div>
              <div className="w-full h-1.5 rounded-full bg-zinc-800 overflow-hidden">
                <div 
                  style={{ width: `${storageStats.usedPercent}%` }}
                  className="bg-purple-500 h-full rounded-full transition-all"
                />
              </div>
              <div className="flex justify-between text-[10px] text-zinc-500 font-mono">
                <span>{storageStats.usedPercent}% Committed</span>
                <span>{storageStats.freeTB} TB Free</span>
              </div>
            </div>
          </div>

          <div className="pt-4 border-t border-zinc-800/80 mt-6 text-[11px] text-zinc-500">
            NFS, VMFS & vSAN datastores aggregated
          </div>
        </div>

      </div>
    </div>
  );
}