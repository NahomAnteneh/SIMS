"use client";

import React, { useState, useEffect } from "react";
import { useRouter } from "next/navigation";
import { LayoutList, LayoutGrid, Loader2 } from "lucide-react";

import { storageColumns, type Storage } from "../columns";
import { DataTable } from "@/components/ui/data-table";
import { StorageCard } from "./card";
import { simsApi } from "@/lib/api/client";

export default function StoragePage() {
  const router = useRouter();
  const [view, setView] = useState<"table" | "card">("table");
  const [datastores, setDatastores] = useState<Storage[]>([]);
  const [loading, setLoading] = useState(true);

  const handleSelectDatastore = (ds: { id: string; name: string }) => {
    router.push(`/compute/vms?datastore_id=${ds.id}&datastore_name=${encodeURIComponent(ds.name)}`);
  };

  useEffect(() => {
    async function loadData() {
      setLoading(true);
      try {
        const rawDss = await simsApi.listDatastores();
        const mapped: Storage[] = rawDss.map((ds) => {
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
            status: (capGB > 0 && freeGB / capGB < 0.1) ? "warning" : "healthy",
          };
        });
        setDatastores(mapped);
      } catch (err) {
        console.error("Failed to load datastores", err);
      } finally {
        setLoading(false);
      }
    }
    loadData();
  }, []);

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between pb-2 border-b border-zinc-800/80">
        <div>
          <h1 className="text-xl font-bold tracking-tight text-zinc-100 flex items-center gap-2">
            Storage & Datastores
            <span className="text-xs font-mono font-normal text-zinc-500">
              ({datastores.length} pools)
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
          <span className="text-sm">Loading storage datastores...</span>
        </div>
      ) : view === "table" ? (
        <DataTable
          columns={storageColumns}
          data={datastores}
          searchColumn="name"
          searchPlaceholder="Filter datastores by name..."
          onRowClick={(row) => handleSelectDatastore(row)}
        />
      ) : (
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-5">
          {datastores.map((ds) => (
            <StorageCard
              key={ds.id}
              storage={ds}
              onClick={() => handleSelectDatastore(ds)}
            />
          ))}
        </div>
      )}
    </div>
  );
}
