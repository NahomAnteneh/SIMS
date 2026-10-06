"use client";

import React, { useState, useEffect } from "react";
import { LayoutList, LayoutGrid } from "lucide-react";

import { netColumns, type NetworkItem } from "../columns";
import { DataTable } from "@/components/ui/data-table";
import { NetworkCard } from "./card";
import { InfraService } from "@/lib/services/infra";

export default function NetworkingPage() {
  const [view, setView] = useState<"table" | "card">("table");
  const [networks, setNetworks] = useState<NetworkItem[]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    async function loadData() {
      try {
        const data = await InfraService.getNetworks();
        setNetworks(data || []);
      } catch (err) {
        console.error("Failed to load networks", err);
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
          <h1 className="text-2xl font-semibold text-zinc-100">Networking</h1>
          <p className="text-sm text-zinc-500 mt-1">
            Manage virtual switches, port groups, VLANs, and subnets.
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
          columns={netColumns}
          data={networks}
          searchColumn="name"
          searchPlaceholder="Filter networks by name..."
        />
      ) : (
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-5">
          {networks.map((net) => (
            <NetworkCard key={net.id} network={net} />
          ))}
        </div>
      )}
    </div>
  );
}
