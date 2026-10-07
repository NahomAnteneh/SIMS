"use client";

import React, { useState, useEffect } from "react";
import { LayoutList, LayoutGrid, Loader2 } from "lucide-react";

import { columns, type Datacenter } from "./columns";
import { DataTable } from "./data-table";
import { DatacenterCard } from "./card";
import { simsApi } from "@/lib/api/client";

export default function DatacentersPage() {
  const [view, setView] = useState<"table" | "card">("table");
  const [datacenters, setDatacenters] = useState<Datacenter[]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    async function loadData() {
      setLoading(true);
      try {
        const [datacenters, clusters, hosts] = await Promise.all([
          simsApi.listDatacenters(),
          simsApi.listClusters(),
          simsApi.listHosts(),
        ]);

        const clusterCountByDc: Record<string, number> = {};
        for (const cl of clusters) {
          if (cl.datacenter_id) {
            clusterCountByDc[cl.datacenter_id] = (clusterCountByDc[cl.datacenter_id] || 0) + 1;
          }
        }

        const hostCountByDc: Record<string, number> = {};
        for (const h of hosts) {
          if (h.datacenter_id) {
            hostCountByDc[h.datacenter_id] = (hostCountByDc[h.datacenter_id] || 0) + 1;
          }
        }

        const mapped: Datacenter[] = datacenters.map((dc) => ({
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
          vmCount: 0,
          applicationCount: 0,
          clientCount: 1,
          totalCpuGHz: 0,
          usedCpuGHz: 0,
          totalMemoryGB: 0,
          usedMemoryGB: 0,
          totalStorageTB: 0,
          usedStorageTB: 0,
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

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-semibold text-zinc-100">Datacenters</h1>
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
          <span className="text-sm">Loading datacenters...</span>
        </div>
      ) : view === "table" ? (
        <DataTable columns={columns} data={datacenters} />
      ) : (
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-5">
          {datacenters.map((dc) => (
            <DatacenterCard key={dc.id || dc.siteCode} datacenter={dc} />
          ))}
        </div>
      )}
    </div>
  );
}
