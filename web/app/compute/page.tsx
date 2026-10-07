"use client";

import React, { useState, useEffect, useCallback } from "react";
import { LayoutList, LayoutGrid, Loader2 } from "lucide-react";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";

import { vmColumns, type VirtualMachine } from "./vms/columns";
import { VmCard } from "./vms/card";
import { appColumns, type Application } from "./applications/columns";
import { ApplicationCard } from "./applications/card";
import { DataTable } from "@/components/ui/data-table";
import { VMDetailSheet } from "@/components/vm/vm-detail-sheet";
import { simsApi } from "@/lib/api/client";

export default function ComputePage() {
  const [view, setView] = useState<"table" | "card">("table");
  const [loading, setLoading] = useState(true);

  const [vms, setVms] = useState<VirtualMachine[]>([]);
  const [applications, setApplications] = useState<Application[]>([]);
  const [selectedVMId, setSelectedVMId] = useState<string | null>(null);

  // Search & Filter states
  const [limit, setLimit] = useState(20);
  const [searchTerm, setSearchTerm] = useState("");
  const [debouncedSearch, setDebouncedSearch] = useState("");
  const [powerState, setPowerState] = useState<string>("all");

  const [currentCursor, setCurrentCursor] = useState<string | undefined>(undefined);
  const [cursorHistory, setCursorHistory] = useState<(string | undefined)[]>([]);
  const [hasMore, setHasMore] = useState(false);
  const [nextCursor, setNextCursor] = useState<string | undefined>(undefined);

  // Debounce search
  useEffect(() => {
    const timer = setTimeout(() => {
      setDebouncedSearch(searchTerm);
    }, 300);
    return () => clearTimeout(timer);
  }, [searchTerm]);

  // Reset pagination on filter change
  useEffect(() => {
    setCurrentCursor(undefined);
    setCursorHistory([]);
  }, [debouncedSearch, powerState, limit]);

  const loadData = useCallback(
    async (cursor?: string) => {
      setLoading(true);
      try {
        const trimmed = debouncedSearch.trim();
        const hasPower = powerState !== "all";

        if (hasPower && trimmed) {
          const res = await simsApi.searchVMs({
            filter: {
              op: "AND",
              operands: [
                { op: "EQ", field: "power_state", value: powerState },
                { op: "CONTAINS", field: "name", value: trimmed },
              ],
            },
            limit,
            cursor,
          });
          setVms(res.items || []);
          setHasMore(res.hasMore);
          setNextCursor(res.nextCursor);
        } else if (hasPower) {
          const res = await simsApi.searchVMs({
            filter: { op: "EQ", field: "power_state", value: powerState },
            limit,
            cursor,
          });
          setVms(res.items || []);
          setHasMore(res.hasMore);
          setNextCursor(res.nextCursor);
        } else if (trimmed) {
          const res = await simsApi.quickSearchVMs(trimmed, limit, cursor);
          setVms(res.items || []);
          setHasMore(res.hasMore);
          setNextCursor(res.nextCursor);
        } else {
          const res = await simsApi.listVMs({ limit, cursor });
          setVms(res.items || []);
          setHasMore(res.pagination.has_more);
          setNextCursor(res.pagination.next_cursor);
        }
        setApplications([]);
      } catch (error) {
        console.error("Failed to load compute data", error);
      } finally {
        setLoading(false);
      }
    },
    [limit, debouncedSearch, powerState]
  );

  useEffect(() => {
    loadData(currentCursor);
  }, [loadData, currentCursor]);

  const handleNextPage = () => {
    if (!nextCursor) return;
    setCursorHistory((prev) => [...prev, currentCursor]);
    setCurrentCursor(nextCursor);
  };

  const handlePrevPage = () => {
    if (cursorHistory.length === 0) return;
    const prev = cursorHistory[cursorHistory.length - 1];
    setCursorHistory((prevStack) => prevStack.slice(0, prevStack.length - 1));
    setCurrentCursor(prev);
  };

  const handleLimitChange = (newLimit: number) => {
    setLimit(newLimit);
  };

  const filterControl = (
    <div className="flex items-center gap-2">
      <select
        value={powerState}
        onChange={(e) => setPowerState(e.target.value)}
        className="h-9 rounded-md border border-zinc-800 bg-zinc-900/60 px-3 py-1 text-xs text-zinc-300 focus:outline-none focus:border-zinc-700"
      >
        <option value="all">All Power States</option>
        <option value="poweredOn">Powered On</option>
        <option value="poweredOff">Powered Off</option>
        <option value="suspended">Suspended</option>
      </select>
      {(searchTerm || powerState !== "all") && (
        <button
          onClick={() => {
            setSearchTerm("");
            setPowerState("all");
          }}
          className="text-xs text-zinc-400 hover:text-zinc-200 underline decoration-dotted"
        >
          Reset
        </button>
      )}
    </div>
  );

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
          {loading && vms.length === 0 ? (
            <div className="h-64 flex flex-col items-center justify-center gap-3 text-zinc-500">
              <Loader2 className="h-6 w-6 animate-spin text-yellow-500" />
              <span className="text-sm">Loading virtual machines...</span>
            </div>
          ) : view === "table" ? (
            <DataTable
              columns={vmColumns}
              data={vms}
              searchPlaceholder="Search VMs by name, IP, moref, UUID..."
              searchValue={searchTerm}
              onSearchChange={setSearchTerm}
              filterComponent={filterControl}
              onRowClick={(row) => setSelectedVMId(row.id)}
              pagination={{
                hasMore,
                canPrev: cursorHistory.length > 0,
                onNext: handleNextPage,
                onPrev: handlePrevPage,
                limit,
                onLimitChange: handleLimitChange,
              }}
            />
          ) : (
            <div className="space-y-4">
              <div className="flex items-center justify-between p-3 rounded-xl border border-zinc-800 bg-[#0c0c0c] gap-3">
                <input
                  type="text"
                  placeholder="Search VMs by name, IP, moref, UUID..."
                  value={searchTerm}
                  onChange={(e) => setSearchTerm(e.target.value)}
                  className="max-w-sm flex-1 bg-zinc-900/60 border border-zinc-800 rounded-md px-3 py-1.5 text-sm text-zinc-100 placeholder:text-zinc-500 focus:outline-none focus:border-zinc-700"
                />
                {filterControl}
              </div>
              <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-5">
                {vms.map((vm) => (
                  <div key={vm.id} onClick={() => setSelectedVMId(vm.id)} className="cursor-pointer">
                    <VmCard vm={vm} />
                  </div>
                ))}
              </div>
            </div>
          )}
        </TabsContent>

        <TabsContent value="applications" className="mt-4">
          {applications.length > 0 ? (
            view === "table" ? (
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
            )
          ) : (
            <div className="p-12 text-center text-sm text-zinc-500 border border-zinc-800 rounded-xl bg-zinc-950/20">
              No application workloads mapped. Workloads are categorized by vSphere tags and folders in the SIMS inventory.
            </div>
          )}
        </TabsContent>
      </Tabs>

      <VMDetailSheet vmId={selectedVMId} onClose={() => setSelectedVMId(null)} />
    </div>
  );
}
