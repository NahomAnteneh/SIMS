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
        const rawHosts = await simsApi.listHosts();
        const mappedHosts: Host[] = rawHosts.map((h) => ({
          id: h.id,
          name: h.name,
          ip: h.name,
          vcenterId: h.vcenter_id || "",
          clusterId: h.cluster_id || "",
          datacenterId: h.datacenter_id || "",
          version: "ESXi 8.0",
          model: "ESXi Hypervisor",
          manufacturer: "VMware",
          cpuModel: "x86_64",
          physicalCores: 32,
          logicalProcessors: 64,
          cpuGHz: 2.8,
          memoryGB: 256,
          cpuUsagePercent: 0,
          memoryUsagePercent: 0,
          vmCount: 0,
          status: h.connection_state === "connected" ? "connected" : "disconnected",
          uptimeDays: 30,
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
