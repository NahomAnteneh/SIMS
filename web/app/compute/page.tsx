"use client";

import React, { useState, useEffect } from "react";
import { LayoutList, LayoutGrid } from "lucide-react";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";

import { vmColumns, type VirtualMachine } from "./vms/columns";
import { VmCard } from "./vms/card";
import { appColumns, type Application } from "./applications/columns";
import { ApplicationCard } from "./applications/card";
import { DataTable } from "@/components/ui/data-table";
import { InfraService } from "@/lib/services/infra";

export default function ComputePage() {
  const [view, setView] = useState<"table" | "card">("table");
  const [loading, setLoading] = useState(true);

  const [vms, setVms] = useState<VirtualMachine[]>([]);
  const [applications, setApplications] = useState<Application[]>([]);

  useEffect(() => {
    async function fetchData() {
      try {
        const [vmData, appData] = await Promise.all([
          InfraService.getVms(),
          InfraService.getApplications(),
        ]);

        setVms(vmData || []);
        setApplications(appData || []);
      } catch (error) {
        console.error("Failed to load compute data", error);
      } finally {
        setLoading(false);
      }
    }

    fetchData();
  }, []);

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-semibold text-zinc-100">
            Compute & Workloads
          </h1>
          <p className="text-sm text-zinc-500 mt-1">
            Manage virtual machines, containerized workloads, applications, and tenant services.
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

      <Tabs defaultValue="vms" className="w-full">
        <TabsList variant="line">
          <TabsTrigger value="vms">Virtual Machines ({vms.length})</TabsTrigger>
          <TabsTrigger value="applications">Applications & Services ({applications.length})</TabsTrigger>
        </TabsList>

        <TabsContent value="vms" className="mt-4">
          {view === "table" ? (
            <DataTable
              columns={vmColumns}
              data={vms}
              searchColumn="name"
              searchPlaceholder="Filter virtual machines by name..."
            />
          ) : (
            <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-5">
              {vms.map((vm) => (
                <VmCard key={vm.id} vm={vm} />
              ))}
            </div>
          )}
        </TabsContent>

        <TabsContent value="applications" className="mt-4">
          {view === "table" ? (
            <DataTable
              columns={appColumns}
              data={applications}
              searchColumn="name"
              searchPlaceholder="Filter applications by name..."
            />
          ) : (
            <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-5">
              {applications.map((app) => (
                <ApplicationCard key={app.id} app={app} />
              ))}
            </div>
          )}
        </TabsContent>
      </Tabs>
    </div>
  );
}
