"use client";

import React, { useEffect, useState, useCallback } from "react";
import Link from "next/link";
import {
  LayoutDashboard,
  Monitor,
  Server,
  HardDrive,
  Network,
  Database,
  Building2,
  Power,
  Cpu,
  RefreshCw,
  Loader2,
  ArrowRight,
  ShieldCheck,
  Activity,
  Layers,
} from "lucide-react";
import { simsApi } from "@/lib/api/client";
import { TopologyItem, VCenterResponse, VMListItem, FacetBucket } from "@/lib/api/types";
import { Button } from "@/components/ui/button";

interface FleetMetrics {
  totalVMs: number;
  poweredOn: number;
  poweredOff: number;
  suspended: number;
  totalVCPUs: number;
  totalMemoryGB: number;
  datacentersCount: number;
  clustersCount: number;
  hostsCount: number;
  datastoresCount: number;
  networksCount: number;
  vcenters: VCenterResponse[];
}

export default function DashboardPage() {
  const [metrics, setMetrics] = useState<FleetMetrics | null>(null);
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);

  const fetchOverview = useCallback(async () => {
    try {
      const [
        vmsResp,
        facetsResp,
        dcList,
        clList,
        hostList,
        dsList,
        netList,
        vcList,
      ] = await Promise.all([
        simsApi.listVMs({ limit: 100 }).catch(() => ({ items: [] as VMListItem[] })),
        simsApi.getVMFacets("power_state").catch(() => ({ facets: {} as Record<string, FacetBucket[]> })),
        simsApi.listDatacenters().catch(() => [] as TopologyItem[]),
        simsApi.listClusters().catch(() => [] as TopologyItem[]),
        simsApi.listHosts().catch(() => [] as TopologyItem[]),
        simsApi.listDatastores().catch(() => [] as TopologyItem[]),
        simsApi.listNetworks().catch(() => [] as TopologyItem[]),
        simsApi.listVCenters().catch(() => [] as VCenterResponse[]),
      ]);

      // Calculate power states from facets or loaded VMs
      const powerBuckets: FacetBucket[] = facetsResp.facets?.power_state || [];
      let on = 0;
      let off = 0;
      let susp = 0;
      if (Array.isArray(powerBuckets)) {
        powerBuckets.forEach((b) => {
          if (b.value === "poweredOn") on = b.count;
          else if (b.value === "poweredOff") off = b.count;
          else if (b.value === "suspended") susp = b.count;
        });
      }

      // Calculate compute aggregates from loaded sample or facet data
      let vcpus = 0;
      let memMB = 0;
      if (vmsResp.items && vmsResp.items.length > 0) {
        vmsResp.items.forEach((vm) => {
          vcpus += vm.cpu_cores || 0;
          memMB += vm.memory_mb || 0;
        });
        if (on === 0 && off === 0 && susp === 0) {
          vmsResp.items.forEach((vm) => {
            if (vm.power_state === "poweredOn") on++;
            else if (vm.power_state === "suspended") susp++;
            else off++;
          });
        }
      }

      const totalCount = on + off + susp || vmsResp.items.length;

      setMetrics({
        totalVMs: totalCount,
        poweredOn: on,
        poweredOff: off,
        suspended: susp,
        totalVCPUs: vcpus,
        totalMemoryGB: Math.round(memMB / 1024),
        datacentersCount: dcList.length,
        clustersCount: clList.length,
        hostsCount: hostList.length,
        datastoresCount: dsList.length,
        networksCount: netList.length,
        vcenters: vcList,
      });
    } catch (err) {
      console.error("Failed to load overview data:", err);
    } finally {
      setLoading(false);
      setRefreshing(false);
    }
  }, []);

  useEffect(() => {
    setLoading(true);
    fetchOverview();
  }, [fetchOverview]);

  const handleRefresh = () => {
    setRefreshing(true);
    fetchOverview();
  };

  return (
    <div className="space-y-4 w-full">
      {/* Page Header */}
      <div className="flex items-center justify-between pb-2 border-b border-zinc-800/80">
        <div className="flex items-center gap-2.5">
          <div className="p-1.5 bg-yellow-500/10 border border-yellow-500/20 text-yellow-500">
            <LayoutDashboard className="h-4 w-4" />
          </div>
          <div>
            <h1 className="text-xl font-bold tracking-tight text-zinc-100 flex items-center gap-2">
              Fleet Overview & Platform Telemetry
            </h1>
            <p className="text-[11px] font-mono text-zinc-500 mt-0.5">
              Real-time vSphere inventory state across all synchronized vCenter endpoints
            </p>
          </div>
        </div>

        <div className="flex items-center gap-2">
          <div className="hidden sm:flex items-center gap-2 px-2.5 py-1 bg-zinc-900/60 border border-zinc-800 font-mono text-xs">
            <span className="w-2 h-2 rounded-none bg-emerald-500 animate-pulse" />
            <span className="text-zinc-400">Stream:</span>
            <span className="text-emerald-400 font-semibold">Active</span>
          </div>

          <Button
            variant="outline"
            size="sm"
            onClick={handleRefresh}
            disabled={refreshing || loading}
            className="h-8 text-xs rounded-none border-zinc-800 bg-zinc-900/40 text-zinc-300 hover:text-white hover:bg-zinc-800 gap-1.5"
          >
            <RefreshCw className={`h-3.5 w-3.5 ${refreshing ? "animate-spin" : ""}`} />
            Refresh
          </Button>
        </div>
      </div>

      {loading ? (
        <div className="h-64 flex flex-col items-center justify-center gap-3 text-zinc-500">
          <Loader2 className="h-6 w-6 animate-spin text-yellow-500" />
          <span className="text-xs font-mono">Aggregating infrastructure telemetry...</span>
        </div>
      ) : metrics ? (
        <div className="space-y-4 w-full">
          {/* Top High-Density Metric Strip */}
          <div className="grid grid-cols-2 md:grid-cols-3 lg:grid-cols-6 gap-2.5">
            {/* Total VMs */}
            <Link
              href="/compute/vms"
              className="p-3 border border-zinc-800 bg-[#0a0a0a] hover:border-yellow-600/50 hover:bg-zinc-900/40 transition-colors block"
            >
              <div className="flex items-center justify-between text-zinc-500 mb-1">
                <span className="text-[10px] font-mono uppercase tracking-wider font-semibold">
                  Virtual Machines
                </span>
                <Monitor className="h-3.5 w-3.5 text-yellow-500" />
              </div>
              <div className="text-xl font-bold font-mono text-zinc-100">
                {metrics.totalVMs}
              </div>
              <div className="text-[10px] font-mono text-emerald-400 mt-1 flex items-center gap-1">
                <Power className="h-2.5 w-2.5" />
                <span>{metrics.poweredOn} active</span>
              </div>
            </Link>

            {/* ESXi Hosts */}
            <Link
              href="/infrastructure/hosts"
              className="p-3 border border-zinc-800 bg-[#0a0a0a] hover:border-zinc-700 hover:bg-zinc-900/40 transition-colors block"
            >
              <div className="flex items-center justify-between text-zinc-500 mb-1">
                <span className="text-[10px] font-mono uppercase tracking-wider font-semibold">
                  ESXi Hosts
                </span>
                <Server className="h-3.5 w-3.5 text-blue-400" />
              </div>
              <div className="text-xl font-bold font-mono text-zinc-100">
                {metrics.hostsCount}
              </div>
              <div className="text-[10px] font-mono text-zinc-500 mt-1">
                Across {metrics.clustersCount} clusters
              </div>
            </Link>

            {/* Storage Pools */}
            <Link
              href="/infrastructure/storage"
              className="p-3 border border-zinc-800 bg-[#0a0a0a] hover:border-zinc-700 hover:bg-zinc-900/40 transition-colors block"
            >
              <div className="flex items-center justify-between text-zinc-500 mb-1">
                <span className="text-[10px] font-mono uppercase tracking-wider font-semibold">
                  Datastores
                </span>
                <HardDrive className="h-3.5 w-3.5 text-amber-400" />
              </div>
              <div className="text-xl font-bold font-mono text-zinc-100">
                {metrics.datastoresCount}
              </div>
              <div className="text-[10px] font-mono text-zinc-500 mt-1">
                VMFS / vSAN / NFS
              </div>
            </Link>

            {/* Networks */}
            <Link
              href="/infrastructure/networking"
              className="p-3 border border-zinc-800 bg-[#0a0a0a] hover:border-zinc-700 hover:bg-zinc-900/40 transition-colors block"
            >
              <div className="flex items-center justify-between text-zinc-500 mb-1">
                <span className="text-[10px] font-mono uppercase tracking-wider font-semibold">
                  Port Groups
                </span>
                <Network className="h-3.5 w-3.5 text-cyan-400" />
              </div>
              <div className="text-xl font-bold font-mono text-zinc-100">
                {metrics.networksCount}
              </div>
              <div className="text-[10px] font-mono text-zinc-500 mt-1">
                VLAN multiranges
              </div>
            </Link>

            {/* Datacenters */}
            <Link
              href="/infrastructure/datacenters"
              className="p-3 border border-zinc-800 bg-[#0a0a0a] hover:border-zinc-700 hover:bg-zinc-900/40 transition-colors block"
            >
              <div className="flex items-center justify-between text-zinc-500 mb-1">
                <span className="text-[10px] font-mono uppercase tracking-wider font-semibold">
                  Datacenters
                </span>
                <Building2 className="h-3.5 w-3.5 text-purple-400" />
              </div>
              <div className="text-xl font-bold font-mono text-zinc-100">
                {metrics.datacentersCount}
              </div>
              <div className="text-[10px] font-mono text-zinc-500 mt-1">
                Topology roots
              </div>
            </Link>

            {/* vCenters */}
            <Link
              href="/admin/data-sources"
              className="p-3 border border-zinc-800 bg-[#0a0a0a] hover:border-zinc-700 hover:bg-zinc-900/40 transition-colors block"
            >
              <div className="flex items-center justify-between text-zinc-500 mb-1">
                <span className="text-[10px] font-mono uppercase tracking-wider font-semibold">
                  vCenters
                </span>
                <Database className="h-3.5 w-3.5 text-emerald-400" />
              </div>
              <div className="text-xl font-bold font-mono text-zinc-100">
                {metrics.vcenters.length}
              </div>
              <div className="text-[10px] font-mono text-emerald-400 mt-1">
                All connected
              </div>
            </Link>
          </div>

          {/* Section 2: Workload Power State & Compute Aggregation */}
          <div className="grid grid-cols-1 lg:grid-cols-3 gap-3">
            {/* Power State Breakdown */}
            <div className="border border-zinc-800 bg-[#0a0a0a] p-4 space-y-3">
              <div className="flex items-center justify-between border-b border-zinc-850 pb-2">
                <h3 className="text-xs font-mono font-bold uppercase tracking-wider text-zinc-200 flex items-center gap-2">
                  <Activity className="h-3.5 w-3.5 text-yellow-500" />
                  Workload Power Distribution
                </h3>
                <Link
                  href="/compute/vms"
                  className="text-[11px] font-mono text-yellow-500 hover:underline flex items-center gap-1"
                >
                  View Fleet <ArrowRight className="h-3 w-3" />
                </Link>
              </div>

              <div className="space-y-2.5 pt-1">
                {/* Powered On */}
                <div className="space-y-1">
                  <div className="flex justify-between text-xs font-mono">
                    <span className="text-zinc-300 flex items-center gap-1.5">
                      <span className="w-2 h-2 rounded-none bg-emerald-500" />
                      poweredOn
                    </span>
                    <span className="font-bold text-zinc-100">{metrics.poweredOn}</span>
                  </div>
                  <div className="h-1.5 w-full bg-zinc-900 overflow-hidden">
                    <div
                      className="h-full bg-emerald-500"
                      style={{
                        width: `${metrics.totalVMs > 0 ? (metrics.poweredOn / metrics.totalVMs) * 100 : 0}%`,
                      }}
                    />
                  </div>
                </div>

                {/* Powered Off */}
                <div className="space-y-1">
                  <div className="flex justify-between text-xs font-mono">
                    <span className="text-zinc-400 flex items-center gap-1.5">
                      <span className="w-2 h-2 rounded-none bg-zinc-600" />
                      poweredOff
                    </span>
                    <span className="font-bold text-zinc-300">{metrics.poweredOff}</span>
                  </div>
                  <div className="h-1.5 w-full bg-zinc-900 overflow-hidden">
                    <div
                      className="h-full bg-zinc-600"
                      style={{
                        width: `${metrics.totalVMs > 0 ? (metrics.poweredOff / metrics.totalVMs) * 100 : 0}%`,
                      }}
                    />
                  </div>
                </div>

                {/* Suspended */}
                <div className="space-y-1">
                  <div className="flex justify-between text-xs font-mono">
                    <span className="text-amber-400 flex items-center gap-1.5">
                      <span className="w-2 h-2 rounded-none bg-amber-500" />
                      suspended
                    </span>
                    <span className="font-bold text-amber-300">{metrics.suspended}</span>
                  </div>
                  <div className="h-1.5 w-full bg-zinc-900 overflow-hidden">
                    <div
                      className="h-full bg-amber-500"
                      style={{
                        width: `${metrics.totalVMs > 0 ? (metrics.suspended / metrics.totalVMs) * 100 : 0}%`,
                      }}
                    />
                  </div>
                </div>
              </div>
            </div>

            {/* Compute Footprint */}
            <div className="border border-zinc-800 bg-[#0a0a0a] p-4 space-y-3">
              <div className="flex items-center justify-between border-b border-zinc-850 pb-2">
                <h3 className="text-xs font-mono font-bold uppercase tracking-wider text-zinc-200 flex items-center gap-2">
                  <Cpu className="h-3.5 w-3.5 text-blue-400" />
                  Fleet Compute Footprint
                </h3>
                <span className="text-[10px] font-mono text-zinc-500 uppercase">Allocated</span>
              </div>

              <div className="grid grid-cols-2 gap-2 pt-1">
                <div className="p-3 bg-zinc-900/60 border border-zinc-800 text-center">
                  <div className="text-[10px] font-mono text-zinc-500 uppercase mb-1">
                    vCPU Cores
                  </div>
                  <div className="text-xl font-bold font-mono text-zinc-100">
                    {metrics.totalVCPUs}
                  </div>
                  <div className="text-[10px] font-mono text-zinc-400 mt-1">Cores active</div>
                </div>

                <div className="p-3 bg-zinc-900/60 border border-zinc-800 text-center">
                  <div className="text-[10px] font-mono text-zinc-500 uppercase mb-1">
                    RAM Footprint
                  </div>
                  <div className="text-xl font-bold font-mono text-zinc-100">
                    {metrics.totalMemoryGB} GB
                  </div>
                  <div className="text-[10px] font-mono text-zinc-400 mt-1">
                    {(metrics.totalMemoryGB / 1024).toFixed(1)} TB memory
                  </div>
                </div>
              </div>

              <div className="p-2.5 bg-black border border-zinc-850 text-[11px] font-mono text-zinc-400 flex items-center justify-between">
                <span>Avg Memory / VM:</span>
                <span className="text-zinc-200 font-bold">
                  {metrics.totalVMs > 0
                    ? `${(metrics.totalMemoryGB / metrics.totalVMs).toFixed(1)} GB`
                    : "0 GB"}
                </span>
              </div>
            </div>

            {/* Quick Topology Drill-Downs */}
            <div className="border border-zinc-800 bg-[#0a0a0a] p-4 space-y-3">
              <div className="flex items-center justify-between border-b border-zinc-850 pb-2">
                <h3 className="text-xs font-mono font-bold uppercase tracking-wider text-zinc-200 flex items-center gap-2">
                  <Layers className="h-3.5 w-3.5 text-purple-400" />
                  Topology Navigation
                </h3>
                <Link
                  href="/topology"
                  className="text-[11px] font-mono text-yellow-500 hover:underline flex items-center gap-1"
                >
                  Graph <ArrowRight className="h-3 w-3" />
                </Link>
              </div>

              <div className="space-y-1.5 pt-1">
                <Link
                  href="/infrastructure/datacenters"
                  className="flex items-center justify-between p-2 bg-zinc-900/40 hover:bg-zinc-800/60 border border-zinc-800/80 text-xs transition-colors"
                >
                  <span className="text-zinc-300 flex items-center gap-2 font-mono">
                    <Building2 className="h-3.5 w-3.5 text-zinc-400" />
                    Datacenters Hierarchy
                  </span>
                  <span className="font-mono text-zinc-500">{metrics.datacentersCount} roots</span>
                </Link>

                <Link
                  href="/infrastructure/hosts"
                  className="flex items-center justify-between p-2 bg-zinc-900/40 hover:bg-zinc-800/60 border border-zinc-800/80 text-xs transition-colors"
                >
                  <span className="text-zinc-300 flex items-center gap-2 font-mono">
                    <Server className="h-3.5 w-3.5 text-zinc-400" />
                    ESXi Host Clusters
                  </span>
                  <span className="font-mono text-zinc-500">{metrics.hostsCount} hypervisors</span>
                </Link>

                <Link
                  href="/infrastructure/storage"
                  className="flex items-center justify-between p-2 bg-zinc-900/40 hover:bg-zinc-800/60 border border-zinc-800/80 text-xs transition-colors"
                >
                  <span className="text-zinc-300 flex items-center gap-2 font-mono">
                    <HardDrive className="h-3.5 w-3.5 text-zinc-400" />
                    Storage Datastores
                  </span>
                  <span className="font-mono text-zinc-500">{metrics.datastoresCount} stores</span>
                </Link>

                <Link
                  href="/infrastructure/networking"
                  className="flex items-center justify-between p-2 bg-zinc-900/40 hover:bg-zinc-800/60 border border-zinc-800/80 text-xs transition-colors"
                >
                  <span className="text-zinc-300 flex items-center gap-2 font-mono">
                    <Network className="h-3.5 w-3.5 text-zinc-400" />
                    VLAN & DVS Networks
                  </span>
                  <span className="font-mono text-zinc-500">{metrics.networksCount} groups</span>
                </Link>
              </div>
            </div>
          </div>

          {/* Section 3: Ingestion Sources & Sync State */}
          <div className="border border-zinc-800 bg-[#0a0a0a] p-4 space-y-3">
            <div className="flex items-center justify-between border-b border-zinc-850 pb-2">
              <h3 className="text-xs font-mono font-bold uppercase tracking-wider text-zinc-200 flex items-center gap-2">
                <Database className="h-3.5 w-3.5 text-emerald-400" />
                Connected vCenter Endpoints & PropertyCollector Feeds
              </h3>
              <Link
                href="/admin/data-sources"
                className="text-[11px] font-mono text-yellow-500 hover:underline flex items-center gap-1"
              >
                Manage Endpoints <ArrowRight className="h-3 w-3" />
              </Link>
            </div>

            {metrics.vcenters.length === 0 ? (
              <div className="p-8 text-center text-xs font-mono text-zinc-500 border border-zinc-800/80">
                No vCenter endpoints registered. Visit Data Sources to configure endpoints.
              </div>
            ) : (
              <div className="overflow-x-auto">
                <table className="w-full text-left text-xs font-mono text-zinc-300">
                  <thead className="border-b border-zinc-800 bg-zinc-950/60 text-[10px] text-zinc-500 uppercase tracking-wider">
                    <tr>
                      <th className="py-2.5 px-3">Endpoint Name</th>
                      <th className="py-2.5 px-3">Host / Port</th>
                      <th className="py-2.5 px-3">vSphere Version</th>
                      <th className="py-2.5 px-3">Collector Stream</th>
                      <th className="py-2.5 px-3">Sync Status</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-zinc-850">
                    {metrics.vcenters.map((vc) => (
                      <tr key={vc.id} className="hover:bg-zinc-900/30">
                        <td className="py-2.5 px-3 font-semibold text-zinc-100 flex items-center gap-2">
                          <Database className="h-3.5 w-3.5 text-yellow-500" />
                          {vc.name}
                        </td>
                        <td className="py-2.5 px-3 text-zinc-400">
                          {vc.host}:{vc.port}
                        </td>
                        <td className="py-2.5 px-3 text-zinc-300">
                          {vc.version?.version ? `vSphere ${vc.version.version}` : "vSphere 8.0"}
                        </td>
                        <td className="py-2.5 px-3">
                          <span className="inline-flex items-center gap-1.5 px-1.5 py-0.5 bg-emerald-500/10 text-emerald-400 border border-emerald-500/20 text-[10px]">
                            <span className="w-1.5 h-1.5 rounded-none bg-emerald-400" />
                            Streaming
                          </span>
                        </td>
                        <td className="py-2.5 px-3">
                          <span className="text-emerald-400 font-semibold flex items-center gap-1">
                            <ShieldCheck className="h-3.5 w-3.5" />
                            Synchronized
                          </span>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </div>
        </div>
      ) : null}
    </div>
  );
}