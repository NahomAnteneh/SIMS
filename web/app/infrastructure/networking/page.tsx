"use client";

import React, { useState, useEffect } from "react";
import { useRouter } from "next/navigation";
import { LayoutList, LayoutGrid, Loader2 } from "lucide-react";

import { netColumns, type NetworkItem } from "../columns";
import { DataTable } from "@/components/ui/data-table";
import { NetworkCard } from "./card";
import { simsApi } from "@/lib/api/client";
import { NetworkDetailSheet } from "@/components/network/network-detail-sheet";

export default function NetworkingPage() {
  const router = useRouter();
  const [view, setView] = useState<"table" | "card">("table");
  const [networks, setNetworks] = useState<NetworkItem[]>([]);
  const [selectedNetwork, setSelectedNetwork] = useState<NetworkItem | null>(null);
  const [loading, setLoading] = useState(true);

  const handleSelectNetwork = (net: NetworkItem) => {
    setSelectedNetwork(net);
  };

  useEffect(() => {
    async function loadData() {
      setLoading(true);
      try {
        const [rawNets, rawDcs, rawVCenters] = await Promise.all([
          simsApi.listNetworks(),
          simsApi.listDatacenters(),
          simsApi.listVCenters().catch(() => []),
        ]);

        const dcMap: Record<string, string> = {};
        for (const dc of rawDcs) {
          dcMap[dc.id] = dc.name;
        }

        const vcenterMap: Record<string, string> = {};
        for (const vc of rawVCenters) {
          vcenterMap[vc.id] = vc.name;
        }

        const mapped: NetworkItem[] = rawNets.map((net) => ({
          id: net.id,
          name: net.name || "Default Network",
          moref: net.moref || "",
          vcenterId: net.vcenter_id || "",
          vcenterName: vcenterMap[net.vcenter_id] || "Primary vCenter",
          datacenterId: net.datacenter_id || "",
          datacenterName: (net.datacenter_id ? dcMap[net.datacenter_id] : "") || "Datacenter",
          type: net.type || (net.name.toLowerCase().includes("dvs") || net.name.toLowerCase().includes("distributed")
            ? "Distributed Virtual Portgroup"
            : "Standard Network"),
          vlanIds: net.vlan_ids,
          switchName: net.switch_name,
          vmCount: net.vm_count || 0,
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
      <div className="flex items-center justify-between pb-2 border-b border-zinc-800/80">
        <div>
          <h1 className="text-xl font-bold tracking-tight text-zinc-100 flex items-center gap-2">
            Networks & Port Groups
            <span className="text-xs font-mono font-normal text-zinc-500">
              ({networks.length} networks)
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
            <NetworkCard
              key={net.id}
              network={net}
              onClick={() => handleSelectNetwork(net)}
            />
          ))}
        </div>
      )}

      {selectedNetwork && (
        <NetworkDetailSheet
          network={selectedNetwork}
          onClose={() => setSelectedNetwork(null)}
        />
      )}
    </div>
  );
}
