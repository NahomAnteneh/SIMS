"use client";

import React, { useState, useEffect } from "react";
import { LayoutList, LayoutGrid } from "lucide-react";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";

import { clusterColumns, hostColumns, type Cluster, type Host } from "../columns";
import { DataTable } from "@/components/ui/data-table";
import { ClusterCard } from "./card";
import { simsApi } from "@/lib/api/client";
import { Loader2 } from "lucide-react";

export default function ClustersPage() {
  const [view, setView] = useState<"table" | "card">("table");
  const [clusters, setClusters] = useState<Cluster[]>([]);
  const [hosts, setHosts] = useState<Host[]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    async function loadData() {
      setLoading(true);
      try {
        const [rawClusters, rawHosts] = await Promise.all([
          simsApi.listClusters(),
          simsApi.listHosts(),
        ]);

        const hostCountByCluster: Record<string, number> = {};
        for (const h of rawHosts) {
          if (h.cluster_id) {
            hostCountByCluster[h.cluster_id] = (hostCountByCluster[h.cluster_id] || 0) + 1;
          }
        }

        const mappedClusters: Cluster[] = rawClusters.map((cl) => ({
          id: cl.id,
          name: cl.name,
          vcenterId: cl.vcenter_id,
          datacenterId: cl.datacenter_id || "",
          haEnabled: true,
          drsEnabled: true,
          status: "healthy",
          hostCount: hostCountByCluster[cl.id] || 0,
          vmCount: 0,
          totalCpuGHz: 0,
          usedCpuGHz: 0,
          totalMemoryGB: 0,
          usedMemoryGB: 0,
        }));

        const mappedHosts: Host[] = rawHosts.map((h) => ({
          id: h.id,
          name: h.name,
          ip: h.name,
          vcenterId: h.vcenter_id,
          clusterId: h.cluster_id || "",
          datacenterId: h.datacenter_id || "",
          version: "ESXi 8.0",
          model: "ESXi Hypervisor",
          manufacturer: "VMware",
          cpuModel: "x86_64",
          physicalCores: 32,
          logicalProcessors: 64,
          cpuGHz: 2.8,
          memoryGB: 256,
          cpuUsagePercent: 0,
          memoryUsagePercent: 0,
          vmCount: 0,
          status: h.connection_state === "connected" ? "healthy" : "warning",
          powerState: (h.power_state as any) || "poweredOn",
          connectionState: (h.connection_state as any) || "connected",
          uptimeDays: 30,
        }));

        setClusters(mappedClusters);
        setHosts(mappedHosts);
      } catch (err) {
        console.error("Failed to load clusters and hosts", err);
      } finally {
        setLoading(false);
      }
    }
    loadData();
  }, []);

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-semibold text-zinc-100">
            Clusters & Hosts
          </h1>
          <p className="text-sm text-zinc-500 mt-1">
            Manage computing clusters, high availability rules, DRS, and ESXi hypervisors.
          </p>
        </div>

        {/* View Toggle */}
        <div className="flex items-center rounded-lg border border-zinc-800 bg-zinc-900/50 p-1">
          <button
            type="button"
            onClick={() => setView("table")}
            className={`flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium rounded-md transition-colors ${
              view === "table"
                ? "bg-zinc-800 text-zinc-100 shadow-sm"
                : "text-zinc-400 hover:text-zinc-200"
            }`}
          >
            <LayoutList className="h-3.5 w-3.5" />
            Table
          </button>
          <button
            type="button"
            onClick={() => setView("card")}
            className={`flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium rounded-md transition-colors ${
              view === "card"
                ? "bg-zinc-800 text-zinc-100 shadow-sm"
                : "text-zinc-400 hover:text-zinc-200"
            }`}
          >
            <LayoutGrid className="h-3.5 w-3.5" />
            Card
          </button>
        </div>
      </div>

      {loading ? (
        <div className="h-64 flex flex-col items-center justify-center gap-3 text-zinc-500">
          <Loader2 className="h-6 w-6 animate-spin text-yellow-500" />
          <span className="text-sm">Loading clusters & hosts...</span>
        </div>
      ) : (
        <Tabs defaultValue="clusters" className="w-full">
          <TabsList variant="line">
            <TabsTrigger value="clusters">Clusters ({clusters.length})</TabsTrigger>
            <TabsTrigger value="hosts">ESXi Hosts ({hosts.length})</TabsTrigger>
          </TabsList>

          <TabsContent value="clusters" className="mt-4">
            {view === "table" ? (
              <DataTable
                columns={clusterColumns}
                data={clusters}
                searchColumn="name"
                searchPlaceholder="Filter clusters by name..."
              />
            ) : (
              <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-5">
                {clusters.map((cl) => (
                  <ClusterCard key={cl.id} cluster={cl} />
                ))}
              </div>
            )}
          </TabsContent>

          <TabsContent value="hosts" className="mt-4">
            <DataTable
              columns={hostColumns}
              data={hosts}
              searchColumn="name"
              searchPlaceholder="Filter hosts by name..."
            />
          </TabsContent>
        </Tabs>
      )}
    </div>
  );
}
