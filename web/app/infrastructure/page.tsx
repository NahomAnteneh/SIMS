"use client";

import React, { useState } from "react";
import { LayoutList, LayoutGrid } from "lucide-react";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";

import { columns, type Datacenter } from "./columns";
import { DataTable } from "./data-table";
import { DatacenterCard } from "./card";

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
  const [view, setView] = useState<"table" | "card">("table");

  return (
    <div>
      <div className="space-y-6">
        <div className="flex items-center justify-between">
          <div>
            <h1 className="text-2xl font-semibold text-zinc-100">
              Datacenters
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

        <Tabs defaultValue="Datacenters" className="w-[400px]">
          <TabsList>
            <TabsTrigger value="datacenters">Datacenters</TabsTrigger>
            <TabsTrigger value="custers&hosts">Clusters & Hosts</TabsTrigger>
            <TabsTrigger value="storage">Storage</TabsTrigger>
            <TabsTrigger value="networking">Networking</TabsTrigger>
          </TabsList>
          <TabsContent value="account">
            Make changes to your account here.
          </TabsContent>
          <TabsContent value="password">Change your password here.</TabsContent>
          <TabsContent value="account">
            Make changes to your account here.
          </TabsContent>
          <TabsContent value="password">Change your password here.</TabsContent>
        </Tabs>

        {view === "table" ? (
          <DataTable columns={columns} data={sampleDatacenters} />
        ) : (
          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-5">
            {sampleDatacenters.map((dc) => (
              <DatacenterCard key={dc.siteCode} datacenter={dc} />
            ))}
          </div>
        )}
      </div>
    </div>
  );
}
