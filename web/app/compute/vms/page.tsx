"use client";

import React, { useState, useEffect } from "react";
import { LayoutList, LayoutGrid } from "lucide-react";

import { vmColumns, type VirtualMachine } from "./columns";
import { DataTable } from "@/components/ui/data-table";
import { VmCard } from "./card";
import { InfraService } from "@/lib/services/infra";

export default function VmsPage() {
  const [view, setView] = useState<"table" | "card">("table");
  const [vms, setVms] = useState<VirtualMachine[]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    async function loadData() {
      try {
        const data = await InfraService.getVms();
        setVms(data || []);
      } catch (err) {
        console.error("Failed to load virtual machines", err);
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
            Virtual Machines
          </h1>
          <p className="text-sm text-zinc-500 mt-1">
            Manage instances, guest operating systems, specs, and compute workloads.
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
    </div>
  );
}
