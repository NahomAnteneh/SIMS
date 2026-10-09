"use client";

import React, { useState, useEffect } from "react";
import { useRouter } from "next/navigation";
import { LayoutList, LayoutGrid, Loader2 } from "lucide-react";

import { columns, type Datacenter } from "./columns";
import { DataTable } from "@/components/ui/data-table";
import { DatacenterCard } from "./card";
import { simsApi } from "@/lib/api/client";

export default function DatacentersPage() {
  const router = useRouter();
  const [view, setView] = useState<"table" | "card">("table");
  const [datacenters, setDatacenters] = useState<Datacenter[]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    async function loadData() {
      setLoading(true);
      try {
        const [rawDcs, rawClusters, rawHosts, rawDss, rawNets, rawVCenters, facetsRes] = await Promise.all([
          simsApi.listDatacenters(),
          simsApi.listClusters(),
          simsApi.listHosts(),
          simsApi.listDatastores(),
          simsApi.listNetworks(),
          simsApi.listVCenters().catch(() => []),
          simsApi.getVMFacets("datacenter").catch(() => null),
        ]);

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
        for (const cl of rawClusters) {
          let targetDcId = cl.datacenter_id;
          if (!targetDcId && cl.vcenter_id && dcListForVc[cl.vcenter_id]?.length === 1) {
            targetDcId = dcListForVc[cl.vcenter_id][0];
          }
          if (targetDcId) {
            clusterCountByDc[targetDcId] = (clusterCountByDc[targetDcId] || 0) + 1;
          }
        }

        const hostCountByDc: Record<string, number> = {};
        for (const h of rawHosts) {
          let targetDcId = h.datacenter_id;
          if (!targetDcId && h.vcenter_id && dcListForVc[h.vcenter_id]?.length === 1) {
            targetDcId = dcListForVc[h.vcenter_id][0];
          }
          if (targetDcId) {
            hostCountByDc[targetDcId] = (hostCountByDc[targetDcId] || 0) + 1;
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
            storageBytesByDc[targetDcId] = (storageBytesByDc[targetDcId] || 0) + (ds.capacity_bytes || 0);
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
        if (facetsRes && facetsRes.facets) {
          for (const b of facetsRes.facets.datacenter || []) {
            vmCountByDc[b.value] = b.count;
          }
        }

        const mapped: Datacenter[] = rawDcs.map((dc) => ({
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

        setDatacenters(mapped);
      } catch (err) {
        console.error("Failed to load datacenters", err);
      } finally {
        setLoading(false);
      }
    }
    loadData();
  }, []);

  const handleSelectDatacenter = (dc: { id: string; name: string }) => {
    router.push(`/compute/vms?datacenter_id=${dc.id}&datacenter_name=${encodeURIComponent(dc.name)}`);
  };

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between pb-2 border-b border-zinc-800/80">
        <div>
          <h1 className="text-xl font-bold tracking-tight text-zinc-100 flex items-center gap-2">
            Datacenters
            <span className="text-xs font-mono font-normal text-zinc-500">
              ({datacenters.length} sites)
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
          <span className="text-sm">Loading datacenters...</span>
        </div>
      ) : view === "table" ? (
        <DataTable
          columns={columns}
          data={datacenters}
          searchColumn="name"
          searchPlaceholder="Filter datacenters by name..."
          onRowClick={(row) => handleSelectDatacenter(row)}
        />
      ) : (
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-5">
          {datacenters.map((dc) => (
            <DatacenterCard
              key={dc.id}
              datacenter={dc}
              onClick={() => handleSelectDatacenter(dc)}
            />
          ))}
        </div>
      )}
    </div>
  );
}
