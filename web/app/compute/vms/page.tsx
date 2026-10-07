"use client";

import React, { useState, useEffect, useCallback } from "react";
import { LayoutList, LayoutGrid, RefreshCw, Loader2 } from "lucide-react";

import { vmColumns, type VirtualMachine } from "./columns";
import { DataTable } from "@/components/ui/data-table";
import { VmCard } from "./card";
import { VMDetailSheet } from "@/components/vm/vm-detail-sheet";
import { simsApi } from "@/lib/api/client";
import { Button } from "@/components/ui/button";

export default function VmsPage() {
  const [view, setView] = useState<"table" | "card">("table");
  const [vms, setVms] = useState<VirtualMachine[]>([]);
  const [loading, setLoading] = useState(true);
  const [limit, setLimit] = useState(20);
  const [searchTerm, setSearchTerm] = useState("");
  const [debouncedSearch, setDebouncedSearch] = useState("");
  const [powerState, setPowerState] = useState<string>("all");

  const [currentCursor, setCurrentCursor] = useState<string | undefined>(undefined);
  const [cursorHistory, setCursorHistory] = useState<(string | undefined)[]>([]);
  const [hasMore, setHasMore] = useState(false);
  const [nextCursor, setNextCursor] = useState<string | undefined>(undefined);
  const [selectedVMId, setSelectedVMId] = useState<string | null>(null);

  // Debounce search term changes
  useEffect(() => {
    const timer = setTimeout(() => {
      setDebouncedSearch(searchTerm);
    }, 300);
    return () => clearTimeout(timer);
  }, [searchTerm]);

  // Reset pagination on filter / search changes
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
      } catch (err) {
        console.error("Failed to load virtual machines", err);
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
            Virtual Machines
          </h1>
          <p className="text-sm text-zinc-500 mt-1">
            Global inventory of instances, compute specs, IP addresses, and state.
          </p>
        </div>

        <div className="flex items-center gap-3">
          <Button
            variant="outline"
            size="sm"
            onClick={() => loadData(currentCursor)}
            disabled={loading}
            className="border-zinc-800 bg-zinc-900/50 text-zinc-300 hover:text-white hover:bg-zinc-800 gap-1.5 h-8"
          >
            <RefreshCw className={`h-3.5 w-3.5 ${loading ? "animate-spin" : ""}`} />
            Refresh
          </Button>

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
      </div>

      {loading && vms.length === 0 ? (
        <div className="h-64 flex flex-col items-center justify-center gap-3 text-zinc-500">
          <Loader2 className="h-6 w-6 animate-spin text-yellow-600" />
          <span className="text-sm">Fetching virtual machine fleet...</span>
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
          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
            {vms.map((vm) => (
              <VmCard key={vm.id} vm={vm} onClick={() => setSelectedVMId(vm.id)} />
            ))}
          </div>
        </div>
      )}

      {/* VM Detail Flyout Sheet */}
      <VMDetailSheet
        vmId={selectedVMId}
        onClose={() => setSelectedVMId(null)}
      />
    </div>
  );
}
