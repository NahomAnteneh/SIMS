"use client";

import React, { useState, useEffect } from "react";
import { LayoutList, LayoutGrid } from "lucide-react";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";

import { clusterColumns, hostColumns, type Cluster, type Host } from "../columns";
import { DataTable } from "@/components/ui/data-table";
import { ClusterCard } from "./card";
import { InfraService } from "@/lib/services/infra";

export default function ClustersPage() {
  const [view, setView] = useState<"table" | "card">("table");
  const [clusters, setClusters] = useState<Cluster[]>([]);
  const [hosts, setHosts] = useState<Host[]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    async function loadData() {
      try {
        const [clusterData, hostData] = await Promise.all([
          InfraService.getClusters(),
          InfraService.getHosts(),
        ]);
        setClusters(clusterData || []);
        setHosts(hostData || []);
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
    </div>
  );
}
