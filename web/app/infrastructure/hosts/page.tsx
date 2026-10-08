"use client";

import React, { useState, useEffect } from "react";
import { useRouter } from "next/navigation";
import { Loader2, Server } from "lucide-react";
import { hostColumns, type Host } from "../columns";
import { DataTable } from "@/components/ui/data-table";
import { simsApi } from "@/lib/api/client";

export default function HostsPage() {
  const router = useRouter();
  const [hosts, setHosts] = useState<Host[]>([]);
  const [loading, setLoading] = useState(true);

  const handleSelectHost = (h: { id: string; name: string }) => {
    router.push(`/compute/vms?host_id=${h.id}&host_name=${encodeURIComponent(h.name)}`);
  };

  useEffect(() => {
    async function loadData() {
      setLoading(true);
      try {
        const [rawHosts, rawClusters, rawDcs, rawVCenters] = await Promise.all([
          simsApi.listHosts(),
          simsApi.listClusters(),
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

        const clusterNameMap: Record<string, string> = {};
        for (const cl of rawClusters) {
          clusterNameMap[cl.id] = cl.name;
        }

        const mappedHosts: Host[] = rawHosts.map((h) => ({
          id: h.id,
          name: h.name,
          moref: h.moref || "",
          vcenterId: h.vcenter_id || "",
          vcenterName: vcenterMap[h.vcenter_id] || "Primary vCenter",
          clusterId: h.cluster_id || "",
          clusterName: (h.cluster_id ? clusterNameMap[h.cluster_id] : "") || "Standalone",
          datacenterId: h.datacenter_id || "",
          datacenterName: (h.datacenter_id ? dcMap[h.datacenter_id] : "") || "Datacenter",
          vmCount: typeof h.vm_count === "number" ? h.vm_count : 0,
          status: h.connection_state === "connected" ? "connected" : "disconnected",
          powerState: h.power_state || "poweredOn",
          connectionState: h.connection_state || "connected",
        }));

        setHosts(mappedHosts);
      } catch (err) {
        console.error("Failed to load ESXi hosts", err);
      } finally {
        setLoading(false);
      }
    }
    loadData();
  }, []);

  return (
    <div className="space-y-4 w-full">
      <div className="flex items-center justify-between pb-2 border-b border-zinc-800/80">
        <div className="flex items-center gap-2">
          <Server className="h-5 w-5 text-yellow-500" />
          <h1 className="text-xl font-bold tracking-tight text-zinc-100">
            ESXi Hosts
          </h1>
          <span className="text-xs font-mono text-zinc-500 ml-2">
            ({hosts.length} hypervisors)
          </span>
        </div>
      </div>

      {loading ? (
        <div className="h-64 flex flex-col items-center justify-center gap-3 text-zinc-500">
          <Loader2 className="h-6 w-6 animate-spin text-yellow-500" />
          <span className="text-sm">Loading ESXi hosts...</span>
        </div>
      ) : (
        <DataTable
          columns={hostColumns}
          data={hosts}
          searchColumn="name"
          searchPlaceholder="Filter hosts by name, cluster, or status..."
          onRowClick={(row) => handleSelectHost(row)}
        />
      )}
    </div>
  );
}
