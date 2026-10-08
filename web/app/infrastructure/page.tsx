"use client";

import React, { useState, useEffect } from "react";
import { LayoutList, LayoutGrid, Loader2 } from "lucide-react";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";

import {
  dcColumns,
  clusterColumns,
  storageColumns,
  netColumns,
  type Datacenter,
  type Cluster,
  type Storage,
  type NetworkItem,
} from "./columns";
import { DataTable } from "@/components/ui/data-table";
import { DatacenterCard } from "./datacenters/card";
import { ClusterCard } from "./clusters/card";
import { StorageCard } from "./storage/card";
import { NetworkCard } from "./networking/card";
import { simsApi } from "@/lib/api/client";

export default function InfrastructureOverviewPage() {
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
        const [rawDcs, rawCls, rawHosts, rawDss, rawNets, rawVCenters, facetsRes] =
          await Promise.all([
            simsApi.listDatacenters(),
            simsApi.listClusters(),
            simsApi.listHosts(),
            simsApi.listDatastores(),
            simsApi.listNetworks(),
            simsApi.listVCenters().catch(() => []),
            simsApi.getVMFacets("datacenter,cluster").catch(() => null),
          ]);

        const dcMap: Record<string, string> = {};
        for (const dc of rawDcs) {
          dcMap[dc.id] = dc.name;
        }

        const vcenterMap: Record<string, string> = {};
        for (const vc of rawVCenters) {
          vcenterMap[vc.id] = vc.name;
        }

        const dcListForVc: Record<string, string[]> = {};
        for (const dc of rawDcs) {
          if (!dcListForVc[dc.vcenter_id]) {
            dcListForVc[dc.vcenter_id] = [];
          }
          dcListForVc[dc.vcenter_id].push(dc.id);
        }

        const clusterCountByDc: Record<string, number> = {};
        for (const cl of rawCls) {
          let targetDcId = cl.datacenter_id;
          if (!targetDcId && cl.vcenter_id && dcListForVc[cl.vcenter_id]?.length === 1) {
            targetDcId = dcListForVc[cl.vcenter_id][0];
          }
          if (targetDcId) {
            clusterCountByDc[targetDcId] = (clusterCountByDc[targetDcId] || 0) + 1;
          }
        }

        const hostCountByDc: Record<string, number> = {};
        const hostCountByCluster: Record<string, number> = {};
        for (const h of rawHosts) {
          let targetDcId = h.datacenter_id;
          if (!targetDcId && h.vcenter_id && dcListForVc[h.vcenter_id]?.length === 1) {
            targetDcId = dcListForVc[h.vcenter_id][0];
          }
          if (targetDcId) {
            hostCountByDc[targetDcId] = (hostCountByDc[targetDcId] || 0) + 1;
          }
          if (h.cluster_id) {
            hostCountByCluster[h.cluster_id] = (hostCountByCluster[h.cluster_id] || 0) + 1;
          }
        }

        const datastoreCountByDc: Record<string, number> = {};
        const storageBytesByDc: Record<string, number> = {};
        for (const ds of rawDss) {
          let targetDcId = ds.datacenter_id;
          if (!targetDcId && ds.vcenter_id && dcListForVc[ds.vcenter_id]?.length === 1) {
            targetDcId = dcListForVc[ds.vcenter_id][0];
          }
          if (targetDcId) {
            datastoreCountByDc[targetDcId] = (datastoreCountByDc[targetDcId] || 0) + 1;
            storageBytesByDc[targetDcId] =
              (storageBytesByDc[targetDcId] || 0) + (ds.capacity_bytes || 0);
          }
        }

        const networkCountByDc: Record<string, number> = {};
        for (const net of rawNets) {
          let targetDcId = net.datacenter_id;
          if (!targetDcId && net.vcenter_id && dcListForVc[net.vcenter_id]?.length === 1) {
            targetDcId = dcListForVc[net.vcenter_id][0];
          }
          if (targetDcId) {
            networkCountByDc[targetDcId] = (networkCountByDc[targetDcId] || 0) + 1;
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
          moref: dc.moref || "",
          vcenterId: dc.vcenter_id || "",
          vcenterName: vcenterMap[dc.vcenter_id] || "Primary vCenter",
          clusterCount: clusterCountByDc[dc.id] || 0,
          hostCount: hostCountByDc[dc.id] || 0,
          vmCount: vmCountByDc[dc.name] ?? vmCountByDc[dc.id] ?? (rawDcs.length === 1 ? Object.values(vmCountByDc).reduce((a, b) => a + b, 0) : 0),
          datastoreCount: datastoreCountByDc[dc.id] || 0,
          totalStorageBytes: storageBytesByDc[dc.id] || 0,
          networkCount: networkCountByDc[dc.id] || 0,
          status: "healthy",
        }));

        const mappedCls: Cluster[] = rawCls.map((cl) => ({
          id: cl.id,
          name: cl.name,
          moref: cl.moref || "",
          vcenterId: cl.vcenter_id,
          vcenterName: vcenterMap[cl.vcenter_id] || "Primary vCenter",
          datacenterId: cl.datacenter_id || "",
          datacenterName: (cl.datacenter_id ? dcMap[cl.datacenter_id] : "") || "Datacenter",
          haEnabled: true,
          drsEnabled: true,
          status: "healthy",
          hostCount: hostCountByCluster[cl.id] || 0,
          vmCount: vmCountByCluster[cl.name] || 0,
        }));

        const mappedStorage: Storage[] = rawDss.map((ds) => {
          const capGB = ds.capacity_bytes ? Math.round(ds.capacity_bytes / (1024 * 1024 * 1024)) : 0;
          const freeGB = ds.free_bytes ? Math.round(ds.free_bytes / (1024 * 1024 * 1024)) : 0;
          const usedGB = Math.max(0, capGB - freeGB);
          return {
            id: ds.id,
            name: ds.name || "Unnamed Datastore",
            moref: ds.moref || "",
            vcenterId: ds.vcenter_id || "",
            vcenterName: vcenterMap[ds.vcenter_id] || "Primary vCenter",
            datacenterId: ds.datacenter_id || "",
            datacenterName: (ds.datacenter_id ? dcMap[ds.datacenter_id] : "") || "Datacenter",
            type: ds.type || "VMFS",
            capacityGB: capGB,
            usedGB: usedGB,
            freeGB: freeGB,
            vmCount: ds.vm_count || 0,
            status: capGB > 0 && freeGB / capGB < 0.1 ? "warning" : "healthy",
          };
        });

        const mappedNets: NetworkItem[] = rawNets.map((net) => ({
          id: net.id,
          name: net.name || "Default Network",
          moref: net.moref || "",
          vcenterId: net.vcenter_id || "",
          vcenterName: vcenterMap[net.vcenter_id] || "Primary vCenter",
          datacenterId: net.datacenter_id || "",
          datacenterName: (net.datacenter_id ? dcMap[net.datacenter_id] : "") || "Datacenter",
          type:
            net.type ||
            (net.name.toLowerCase().includes("dvs") || net.name.toLowerCase().includes("distributed")
              ? "Distributed Virtual Portgroup"
              : "Standard Network"),
          vlanIds: net.vlan_ids,
          switchName: net.switch_name,
          vmCount: net.vm_count || 0,
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
    <div className="space-y-6">
      <div className="flex items-center justify-between pb-2 border-b border-zinc-800/80">
        <div>
          <h1 className="text-xl font-bold tracking-tight text-zinc-100 flex items-center gap-2">
            Infrastructure Overview
            <span className="text-xs font-mono font-normal text-zinc-500">
              ({datacenters.length} sites, {clusters.length} clusters)
            </span>
          </h1>
        </div>

        {/* View Toggle */}
        <div className="flex items-center rounded-none border border-zinc-800 bg-zinc-900/50 p-1">
          <button
            type="button"
            onClick={() => setView("table")}
            className={`flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium rounded-none transition-colors ${
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
            className={`flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium rounded-none transition-colors ${
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
          <TabsList variant="line">
            <TabsTrigger value="datacenters">Datacenters ({datacenters.length})</TabsTrigger>
            <TabsTrigger value="clusters">Clusters ({clusters.length})</TabsTrigger>
            <TabsTrigger value="storage">Storage ({storage.length})</TabsTrigger>
            <TabsTrigger value="networking">Networking ({networking.length})</TabsTrigger>
          </TabsList>

          <TabsContent value="datacenters" className="mt-4">
            {view === "table" ? (
              <DataTable
                columns={dcColumns}
                data={datacenters}
                searchColumn="name"
                searchPlaceholder="Filter datacenters by name..."
              />
            ) : (
              <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-5">
                {datacenters.map((dc) => (
                  <DatacenterCard key={dc.id} datacenter={dc} />
                ))}
              </div>
            )}
          </TabsContent>

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

          <TabsContent value="storage" className="mt-4">
            {view === "table" ? (
              <DataTable
                columns={storageColumns}
                data={storage}
                searchColumn="name"
                searchPlaceholder="Filter datastores by name..."
              />
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
              <DataTable
                columns={netColumns}
                data={networking}
                searchColumn="name"
                searchPlaceholder="Filter networks by name..."
              />
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
  );
}
