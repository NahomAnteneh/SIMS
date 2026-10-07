"use client";

import React, { useState, useEffect } from "react";
import { LayoutList, LayoutGrid } from "lucide-react";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";

import { dcColumns, clusterColumns, storageColumns, netColumns, type Datacenter, type Cluster, type Storage, type NetworkItem } from "./columns";
import { DataTable } from "./data-table";
import { DatacenterCard } from "./datacenters/card";
import { ClusterCard } from "./clusters/card";
import { StorageCard } from "./storage/card";
import { NetworkCard } from "./networking/card";
import { simsApi } from "@/lib/api/client";
import { Loader2 } from "lucide-react";

export default function DatacentersPage() {
  const [view, setView] = useState<"table" | "card">("table");
  const [loading, setLoading] = useState(true);

  const [datacenters, setDatacenters] = useState<Datacenter[]>([]);
  const [clusters, setClusters] = useState<Cluster[]>([]);
  const [storage, setStorage] = useState<Storage[]>([]);
  const [networking, setNetworking] = useState<NetworkItem[]>([]);

  useEffect(() => {
    async function fetchData() {
      setLoading(true);
      try {
        const [rawDcs, rawCls, rawHosts, rawDss, rawNets, facetsRes] = await Promise.all([
          simsApi.listDatacenters(),
          simsApi.listClusters(),
          simsApi.listHosts(),
          simsApi.listDatastores(),
          simsApi.listNetworks(),
          simsApi.getVMFacets("datacenter,cluster").catch(() => null),
        ]);

        const clusterCountByDc: Record<string, number> = {};
        for (const cl of rawCls) {
          if (cl.datacenter_id) {
            clusterCountByDc[cl.datacenter_id] = (clusterCountByDc[cl.datacenter_id] || 0) + 1;
          }
        }

        const hostCountByDc: Record<string, number> = {};
        const hostCountByCluster: Record<string, number> = {};
        for (const h of rawHosts) {
          if (h.datacenter_id) {
            hostCountByDc[h.datacenter_id] = (hostCountByDc[h.datacenter_id] || 0) + 1;
          }
          if (h.cluster_id) {
            hostCountByCluster[h.cluster_id] = (hostCountByCluster[h.cluster_id] || 0) + 1;
          }
        }

        const vmCountByDc: Record<string, number> = {};
        const vmCountByCluster: Record<string, number> = {};
        if (facetsRes && facetsRes.facets) {
          for (const b of facetsRes.facets.datacenter || []) {
            vmCountByDc[b.value] = b.count;
          }
          for (const b of facetsRes.facets.cluster || []) {
            vmCountByCluster[b.value] = b.count;
          }
        }

        const mappedDcs: Datacenter[] = rawDcs.map((dc) => ({
          id: dc.id,
          name: dc.name,
          displayName: dc.name,
          location: "On-Premises",
          country: "vSphere",
          siteCode: dc.moref || dc.id.slice(0, 5).toUpperCase(),
          environment: "production",
          status: "healthy",
          vcenterCount: 1,
          clusterCount: clusterCountByDc[dc.id] || 0,
          hostCount: hostCountByDc[dc.id] || 0,
          vmCount: vmCountByDc[dc.name] || 0,
          applicationCount: 0,
          clientCount: 1,
          totalCpuGHz: 0,
          usedCpuGHz: 0,
          totalMemoryGB: 0,
          usedMemoryGB: 0,
          totalStorageTB: 0,
          usedStorageTB: 0,
        }));

        const mappedCls: Cluster[] = rawCls.map((cl) => ({
          id: cl.id,
          name: cl.name,
          vcenterId: cl.vcenter_id,
          datacenterId: cl.datacenter_id || "",
          haEnabled: true,
          drsEnabled: true,
          status: "healthy",
          hostCount: hostCountByCluster[cl.id] || 0,
          vmCount: vmCountByCluster[cl.name] || 0,
          totalCpuGHz: 0,
          usedCpuGHz: 0,
          totalMemoryGB: 0,
          usedMemoryGB: 0,
        }));

        const mappedStorage: Storage[] = rawDss.map((ds) => {
          const capGB = ds.capacity_bytes ? Math.round(ds.capacity_bytes / (1024 * 1024 * 1024)) : 0;
          const freeGB = ds.free_bytes ? Math.round(ds.free_bytes / (1024 * 1024 * 1024)) : 0;
          const usedGB = Math.max(0, capGB - freeGB);
          return {
            id: ds.id,
            name: ds.name,
            datacenterId: ds.datacenter_id || "",
            type: ds.type || "VMFS",
            capacityGB: capGB,
            usedGB: usedGB,
            freeGB: freeGB,
            status: "healthy",
          };
        });

        const mappedNets: NetworkItem[] = rawNets.map((net) => ({
          id: net.id,
          name: net.name,
          datacenterId: net.datacenter_id || "",
          type: "Standard",
          vlanId: 0,
          subnet: "VLAN Default",
          status: "active",
        }));

        setDatacenters(mappedDcs);
        setClusters(mappedCls);
        setStorage(mappedStorage);
        setNetworking(mappedNets);
      } catch (error) {
        console.error("Failed to load infrastructure data", error);
      } finally {
        setLoading(false);
      }
    }

    fetchData();
  }, []);

  return (
    <div>
      <div className="space-y-6">
        <div className="flex items-center justify-between">
          <div>
            <h1 className="text-2xl font-semibold text-zinc-100">
              Infrastructure
            </h1>
            <p className="text-sm text-zinc-500 mt-1">
              Manage physical sites and top-level vCenter server instances.
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
            <span className="text-sm">Loading infrastructure inventory...</span>
          </div>
        ) : (
          <Tabs defaultValue="datacenters" className="w-full">
            <TabsList variant={"line"}>
              <TabsTrigger value="datacenters">Datacenters</TabsTrigger>
              <TabsTrigger value="clusters&hosts">Clusters & Hosts</TabsTrigger>
              <TabsTrigger value="storage">Storage</TabsTrigger>
              <TabsTrigger value="networking">Networking</TabsTrigger>
            </TabsList>

            <TabsContent value="datacenters" className="mt-4">
              {view === "table" ? (
                <DataTable columns={dcColumns} data={datacenters} searchColumn="displayName" />
              ) : (
                <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-5">
                  {datacenters.map((dc) => (
                    <DatacenterCard key={dc.id || dc.siteCode} datacenter={dc} />
                  ))}
                </div>
              )}
            </TabsContent>

            <TabsContent value="clusters&hosts" className="mt-4">
              {view === "table" ? (
                <DataTable columns={clusterColumns} data={clusters} searchColumn="name" />
              ) : (
                <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-5">
                  {clusters.map((cl) => (
                    <ClusterCard key={cl.id} cluster={cl} />
                  ))}
                </div>
              )}
            </TabsContent>

            <TabsContent value="storage" className="mt-4">
              {view === "table" ? (
                <DataTable columns={storageColumns} data={storage} searchColumn="name" />
              ) : (
                <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-5">
                  {storage.map((ds) => (
                    <StorageCard key={ds.id} storage={ds} />
                  ))}
                </div>
              )}
            </TabsContent>

            <TabsContent value="networking" className="mt-4">
              {view === "table" ? (
                <DataTable columns={netColumns} data={networking} searchColumn="name" />
              ) : (
                <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-5">
                  {networking.map((net) => (
                    <NetworkCard key={net.id} network={net} />
                  ))}
                </div>
              )}
            </TabsContent>
          </Tabs>
        )}
      </div>
    </div>
  );
}
