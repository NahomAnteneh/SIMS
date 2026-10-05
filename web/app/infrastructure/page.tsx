"use client";

import React, { useState, useEffect } from "react";
import { LayoutList, LayoutGrid } from "lucide-react";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";

import { dcColumns, clusterColumns, storageColumns, netColumns, type Datacenter } from "./columns";
import { DataTable } from "./data-table";
import { DatacenterCard } from "./card";

import {
    InfraService
} from "@/lib/services/infra"

const sampleDatacenters: Datacenter[] = [
  {
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
    clientCount: 7,
    totalCpuGHz: 1536,
    usedCpuGHz: 712,
    totalMemoryGB: 6144,
    usedMemoryGB: 3980,
    totalStorageTB: 240,
    usedStorageTB: 156,
  },
  {
    id: "dc-002",
    name: "DC-Nairobi-02",
    displayName: "Nairobi Secondary Hub",
    location: "Nairobi, Kenya",
    country: "Kenya",
    siteCode: "NBO02",
    environment: "production",
    status: "healthy",
    vcenterCount: 1,
    clusterCount: 3,
    hostCount: 8,
    vmCount: 64,
    applicationCount: 12,
    clientCount: 5,
    totalCpuGHz: 1024,
    usedCpuGHz: 480,
    totalMemoryGB: 4096,
    usedMemoryGB: 2100,
    totalStorageTB: 180,
    usedStorageTB: 92,
  },
];

export default function DatacentersPage() {
    console.log(dcColumns)
  const [view, setView] = useState<"table" | "card">("table");
  const [loading, setLoading] = useState(true);

  const [datacenters, setDatacenters] = useState<Datacenter[]>([]);
  const [clusters, setClusters] = useState<any[]>([]);
  const [storage, setStorage] = useState<any[]>([]);
  const [networking, setNetworking] = useState<any[]>([]);

  useEffect(() => {
    async function fetchData() {
      try {
        const [dcData, clusterData, storageData, netData] = await Promise.all([
          InfraService.getDatacenters(),
          InfraService.getClusters(),
          InfraService.getDatastores(),
          InfraService.getNetworks(),
        ]);

        setDatacenters(dcData);
        setClusters(clusterData);
        setStorage(storageData);
        setNetworking(netData);
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

        <Tabs defaultValue="datacenters" className="w-full">
          <TabsList variant={"line"}>
            <TabsTrigger value="datacenters">Datacenters</TabsTrigger>
            <TabsTrigger value="custers&hosts">Clusters & Hosts</TabsTrigger>
            <TabsTrigger value="storage">Storage</TabsTrigger>
            <TabsTrigger value="networking">Networking</TabsTrigger>
          </TabsList>

          <TabsContent value="datacenters">
            {view === "table" ? (
              <DataTable columns={dcColumns} data={datacenters} searchColumn={dcColumns[0].accessorKey}  />
            ) : (
              <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-5">
                {/* {datacenters.map((dc) => (
                  <DatacenterCard key={dc.siteCode} datacenter={dc} />
                ))} */}
              </div>
            )}
          </TabsContent> <TabsContent value="custers&hosts">
            {view === "table" ? (
              <DataTable columns={clusterColumns} data={clusters} searchColumn={clusterColumns[0].accessorKey} />
            ) : (
              <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-5">
                {/* {clusters.map((dc) => (
                  <DatacenterCard key={dc.siteCode} datacenter={dc} />
                ))} */}
              </div>
            )}
          </TabsContent> <TabsContent value="storage">
            {view === "table" ? (
              <DataTable columns={storageColumns} data={storage} searchColumn={storageColumns[0].accessorKey} />
            ) : (
              <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-5">
                {/* {storage.map((dc) => (
                  <DatacenterCard key={dc.siteCode} datacenter={dc} />
                ))} */}
              </div>
            )}
          </TabsContent> <TabsContent value="networking">
            {view === "table" ? (
              <DataTable columns={netColumns} data={networking} searchColumn={netColumns[0].accessorKey} />
            ) : (
              <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-5">
                {/* {networking.map((dc) => (
                  <DatacenterCard key={dc.siteCode} datacenter={dc} />
                ))} */}
              </div>
            )}
          </TabsContent>

        
        </Tabs>
      </div>
    </div>
  );
}
