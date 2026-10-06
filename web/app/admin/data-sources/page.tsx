"use client";

import React, { useState, useEffect } from "react";
import { LayoutList, LayoutGrid } from "lucide-react";

import { dataSourceColumns, type DataSource } from "./columns";
import { DataTable } from "@/components/ui/data-table";
import { DataSourceCard } from "./card";
import { InfraService } from "@/lib/services/infra";

export default function DataSourcesPage() {
  const [view, setView] = useState<"table" | "card">("table");
  const [dataSources, setDataSources] = useState<DataSource[]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    async function loadData() {
      try {
        const data = await InfraService.getDataSources();
        setDataSources(data || []);
      } catch (err) {
        console.error("Failed to load data sources", err);
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
            Data Sources
          </h1>
          <p className="text-sm text-zinc-500 mt-1">
            Configure integration endpoints, vCenter management planes, and discovery syncs.
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

      {view === "table" ? (
        <DataTable
          columns={dataSourceColumns}
          data={dataSources}
          searchColumn="name"
          searchPlaceholder="Filter data sources by name..."
        />
      ) : (
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-5">
          {dataSources.map((ds) => (
            <DataSourceCard key={ds.id} source={ds} />
          ))}
        </div>
      )}
    </div>
  );
}
