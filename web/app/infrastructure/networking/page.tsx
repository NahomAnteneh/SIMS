"use client";

import React, { useState, useEffect } from "react";
import { useRouter } from "next/navigation";
import { LayoutList, LayoutGrid, Loader2 } from "lucide-react";

import { netColumns, type NetworkItem } from "../columns";
import { DataTable } from "@/components/ui/data-table";
import { NetworkCard } from "./card";
import { simsApi } from "@/lib/api/client";

export default function NetworkingPage() {
  const router = useRouter();
  const [view, setView] = useState<"table" | "card">("table");
  const [networks, setNetworks] = useState<NetworkItem[]>([]);
  const [loading, setLoading] = useState(true);

  const handleSelectNetwork = (net: { id: string; name: string }) => {
    router.push(`/compute/vms?network_id=${net.id}&network_name=${encodeURIComponent(net.name)}`);
  };

  useEffect(() => {
    async function loadData() {
      setLoading(true);
      try {
        const rawNets = await simsApi.listNetworks();
        const mapped: NetworkItem[] = rawNets.map((net) => ({
          id: net.id,
          name: net.name,
          datacenterId: net.datacenter_id || "",
          type: net.name.toLowerCase().includes("dvs") || net.name.toLowerCase().includes("distributed") ? "Distributed" : "Standard",
          vlanId: 0,
          subnet: "VLAN Default",
          status: "active",
        }));
        setNetworks(mapped);
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
        <div className="flex items-center rounded-none border border-zinc-800 bg-zinc-900/50 p-0.5">
          <button
            type="button"
            onClick={() => setView("table")}
            className={`flex items-center gap-1.5 px-2.5 py-1 text-xs font-medium rounded-none transition-colors ${
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
            className={`flex items-center gap-1.5 px-2.5 py-1 text-xs font-medium rounded-none transition-colors ${
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
          <span className="text-sm">Loading networks...</span>
        </div>
      ) : view === "table" ? (
        <DataTable
          columns={netColumns}
          data={networks}
          searchColumn="name"
          searchPlaceholder="Filter networks by name..."
          onRowClick={(row) => handleSelectNetwork(row)}
        />
      ) : (
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-5">
          {networks.map((net) => (
            <NetworkCard key={net.id} network={net} onClick={() => handleSelectNetwork(net)} />
          ))}
        </div>
      )}
    </div>
  );
}
