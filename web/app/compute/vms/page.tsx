"use client";

import React, { useState, useEffect, useCallback, useRef, Suspense } from "react";
import { useSearchParams, useRouter } from "next/navigation";
import {
  LayoutList,
  LayoutGrid,
  RefreshCw,
  Loader2,
  Search,
  X,
  Sparkles,
  Filter,
} from "lucide-react";

import { vmColumns, type VirtualMachine } from "./columns";
import { DataTable } from "@/components/ui/data-table";
import { VmCard } from "./card";
import { VMDetailSheet } from "@/components/vm/vm-detail-sheet";
import { simsApi } from "@/lib/api/client";
import { Button } from "@/components/ui/button";
import { parseSearchQuery, SYNTAX_TOKENS } from "@/lib/search/parser";
import { SearchFilter } from "@/lib/api/types";

interface TopologyFilter {
  type: "datacenter" | "cluster" | "host" | "datastore" | "network" | "folder";
  id: string;
  name: string;
}

function VmsContent() {
  const router = useRouter();
  const searchParams = useSearchParams();
  const searchInputRef = useRef<HTMLInputElement>(null);

  const [view, setView] = useState<"table" | "card">("table");
  const [vms, setVms] = useState<VirtualMachine[]>([]);
  const [loading, setLoading] = useState(true);
  const [limit, setLimit] = useState(20);
  const [searchTerm, setSearchTerm] = useState("");
  const [debouncedSearch, setDebouncedSearch] = useState("");
  const [powerState, setPowerState] = useState<string>("all");
  const [isHelperOpen, setIsHelperOpen] = useState(false);

  const [currentCursor, setCurrentCursor] = useState<string | undefined>(undefined);
  const [cursorHistory, setCursorHistory] = useState<(string | undefined)[]>([]);
  const [hasMore, setHasMore] = useState(false);
  const [nextCursor, setNextCursor] = useState<string | undefined>(undefined);
  const [selectedVMId, setSelectedVMId] = useState<string | null>(null);

  const [topologyFilter, setTopologyFilter] = useState<TopologyFilter | null>(null);

  // Read URL search parameters on mount and when searchParams changes
  useEffect(() => {
    const dsId = searchParams.get("datastore_id");
    const clusterId = searchParams.get("cluster_id");
    const hostId = searchParams.get("host_id");
    const dcId = searchParams.get("datacenter_id");
    const netId = searchParams.get("network_id");
    const folderId = searchParams.get("folder_id");

    if (dsId) {
      setTopologyFilter({
        type: "datastore",
        id: dsId,
        name: searchParams.get("datastore_name") || dsId,
      });
    } else if (clusterId) {
      setTopologyFilter({
        type: "cluster",
        id: clusterId,
        name: searchParams.get("cluster_name") || clusterId,
      });
    } else if (hostId) {
      setTopologyFilter({
        type: "host",
        id: hostId,
        name: searchParams.get("host_name") || hostId,
      });
    } else if (dcId) {
      setTopologyFilter({
        type: "datacenter",
        id: dcId,
        name: searchParams.get("datacenter_name") || dcId,
      });
    } else if (netId) {
      setTopologyFilter({
        type: "network",
        id: netId,
        name: searchParams.get("network_name") || netId,
      });
    } else if (folderId) {
      setTopologyFilter({
        type: "folder",
        id: folderId,
        name: searchParams.get("folder_name") || folderId,
      });
    } else {
      setTopologyFilter(null);
    }

    const q = searchParams.get("q");
    if (q) {
      setSearchTerm(q);
      setDebouncedSearch(q);
    }
  }, [searchParams]);

  // Debounce search term changes (250ms)
  useEffect(() => {
    const timer = setTimeout(() => {
      setDebouncedSearch(searchTerm);
    }, 250);
    return () => clearTimeout(timer);
  }, [searchTerm]);

  // Reset pagination on filter / search changes
  useEffect(() => {
    setCurrentCursor(undefined);
    setCursorHistory([]);
  }, [debouncedSearch, powerState, limit, topologyFilter]);

  const loadData = useCallback(
    async (cursor?: string) => {
      setLoading(true);
      try {
        const trimmed = debouncedSearch.trim();
        const hasPower = powerState !== "all";

        if (trimmed) {
          const parsed = parseSearchQuery(trimmed);

          if (parsed.isStructured && parsed.filter) {
            let finalFilter = parsed.filter;
            const extraOperands: SearchFilter[] = [];

            if (hasPower) {
              extraOperands.push({ op: "EQ", field: "power_state", value: powerState });
            }
            if (topologyFilter) {
              const fieldName = `${topologyFilter.type}_id`;
              extraOperands.push({ op: "EQ", field: fieldName, value: topologyFilter.id });
            }

            if (extraOperands.length > 0) {
              const existingOperands =
                finalFilter.op === "AND" && finalFilter.operands ? finalFilter.operands : [finalFilter];
              finalFilter = {
                op: "AND",
                operands: [...existingOperands, ...extraOperands],
              };
            }

            const res = await simsApi.structuredSearchVMs({
              filter: finalFilter,
              limit,
              cursor,
            });
            setVms(res.items || []);
            setHasMore(res.hasMore);
            setNextCursor(res.nextCursor);
          } else {
            // Free-text query
            if (topologyFilter || hasPower) {
              const operands: SearchFilter[] = [
                { op: "CONTAINS", field: "name", value: trimmed },
              ];
              if (hasPower) {
                operands.push({ op: "EQ", field: "power_state", value: powerState });
              }
              if (topologyFilter) {
                operands.push({ op: "EQ", field: `${topologyFilter.type}_id`, value: topologyFilter.id });
              }

              const res = await simsApi.searchVMs({
                filter: { op: "AND", operands },
                limit,
                cursor,
              });
              setVms(res.items || []);
              setHasMore(res.hasMore);
              setNextCursor(res.nextCursor);
            } else {
              const res = await simsApi.quickSearchVMs(trimmed, limit, cursor);
              setVms(res.items || []);
              setHasMore(res.hasMore);
              setNextCursor(res.nextCursor);
            }
          }
        } else if (topologyFilter) {
          if (hasPower) {
            const res = await simsApi.searchVMs({
              filter: {
                op: "AND",
                operands: [
                  { op: "EQ", field: `${topologyFilter.type}_id`, value: topologyFilter.id },
                  { op: "EQ", field: "power_state", value: powerState },
                ],
              },
              limit,
              cursor,
            });
            setVms(res.items || []);
            setHasMore(res.hasMore);
            setNextCursor(res.nextCursor);
          } else {
            const res = await simsApi.listTopologyVMs(topologyFilter.type, topologyFilter.id, {
              limit,
              cursor,
            });
            setVms(res.items || []);
            setHasMore(res.pagination.has_more);
            setNextCursor(res.pagination.next_cursor);
          }
        } else if (hasPower) {
          const res = await simsApi.searchVMs({
            filter: { op: "EQ", field: "power_state", value: powerState },
            limit,
            cursor,
          });
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
    [limit, debouncedSearch, powerState, topologyFilter]
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

  const handleClearTopologyFilter = () => {
    setTopologyFilter(null);
    router.replace("/compute/vms");
  };

  const handleInsertToken = (prefix: string) => {
    setSearchTerm((prev) => {
      const trimmed = prev.trim();
      return trimmed ? `${trimmed} ${prefix}` : prefix;
    });
    searchInputRef.current?.focus();
  };

  const handleSearchKeyDown = (e: React.KeyboardEvent<HTMLInputElement>) => {
    if (e.key === "Enter") {
      e.preventDefault();
      setDebouncedSearch(searchTerm);
      setIsHelperOpen(false);
    } else if (e.key === "Escape") {
      setIsHelperOpen(false);
    }
  };

  const searchBarControl = (
    <div className="relative flex-1 max-w-xl">
      <div className="relative flex items-center">
        <Search className="absolute left-3 h-4 w-4 text-zinc-500 pointer-events-none" />
        <input
          ref={searchInputRef}
          type="text"
          value={searchTerm}
          onChange={(e) => setSearchTerm(e.target.value)}
          onFocus={() => setIsHelperOpen(true)}
          onKeyDown={handleSearchKeyDown}
          placeholder="Search VMs or use syntax: vm:web ip:10.0.0.1/24 power:on ds:vsan..."
          className="w-full pl-9 pr-16 py-1.5 bg-zinc-900/70 border border-zinc-800 rounded-lg text-sm text-zinc-100 placeholder:text-zinc-500 focus:outline-none focus:border-yellow-600/60 focus:ring-1 focus:ring-yellow-600/40 transition-colors"
        />
        <div className="absolute right-2 flex items-center gap-1">
          {searchTerm && (
            <button
              type="button"
              onClick={() => {
                setSearchTerm("");
                setDebouncedSearch("");
              }}
              className="p-1 text-zinc-500 hover:text-zinc-300 rounded transition-colors"
              title="Clear search"
            >
              <X className="h-3.5 w-3.5" />
            </button>
          )}
          <button
            type="button"
            onClick={() => setIsHelperOpen(!isHelperOpen)}
            className={`p-1 rounded text-xs transition-colors ${
              isHelperOpen ? "text-yellow-400 bg-yellow-500/10" : "text-zinc-500 hover:text-zinc-300"
            }`}
            title="Toggle search syntax helper"
          >
            <Sparkles className="h-3.5 w-3.5" />
          </button>
        </div>
      </div>

      {/* Syntax Helper Dropdown Popover */}
      {isHelperOpen && (
        <div className="absolute left-0 top-full mt-2 w-full p-3 rounded-xl border border-zinc-800 bg-[#0e0e0e] shadow-2xl z-40">
          <div className="flex items-center justify-between pb-2 mb-2 border-b border-zinc-800/80">
            <span className="text-[11px] font-semibold uppercase tracking-wider text-zinc-400 flex items-center gap-1.5">
              <Sparkles className="h-3 w-3 text-yellow-500" />
              Advanced Search Syntax
            </span>
            <button
              type="button"
              onClick={() => setIsHelperOpen(false)}
              className="text-xs text-zinc-500 hover:text-zinc-300"
            >
              Close
            </button>
          </div>
          <div className="flex flex-wrap gap-1.5 mb-2.5">
            {SYNTAX_TOKENS.map((token) => (
              <button
                key={token.prefix}
                type="button"
                onClick={() => handleInsertToken(token.prefix)}
                className="px-2 py-1 rounded bg-zinc-900 border border-zinc-800 text-xs font-mono text-yellow-400 hover:border-yellow-600/60 hover:bg-zinc-800/80 transition-colors flex items-center gap-1"
              >
                <span>{token.prefix}</span>
                <span className="text-[10px] text-zinc-400 font-sans">({token.label})</span>
              </button>
            ))}
          </div>
          <p className="text-[11px] text-zinc-500">
            Combine tokens with free-text (e.g. <code className="text-zinc-300 font-mono">vm:api power:on ip:10.0.0.0/24</code>). Press Enter to apply.
          </p>
        </div>
      )}
    </div>
  );

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
      {(searchTerm || powerState !== "all" || topologyFilter) && (
        <button
          onClick={() => {
            setSearchTerm("");
            setDebouncedSearch("");
            setPowerState("all");
            if (topologyFilter) handleClearTopologyFilter();
          }}
          className="text-xs text-zinc-400 hover:text-zinc-200 underline decoration-dotted whitespace-nowrap"
        >
          Reset All
        </button>
      )}
    </div>
  );

  return (
    <div className="space-y-5">
      {/* Page Header */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-semibold text-zinc-100 flex items-center gap-2.5">
            Virtual Machines
            {topologyFilter && (
              <span className="text-xs font-normal px-2.5 py-0.5 rounded-full bg-yellow-500/10 border border-yellow-500/30 text-yellow-400">
                Filtered by {topologyFilter.type}
              </span>
            )}
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

      {/* Topology Active Filter Chip Banner */}
      {topologyFilter && (
        <div className="flex items-center justify-between p-3 px-4 rounded-xl border border-yellow-600/40 bg-yellow-950/20 text-yellow-200 shadow-sm">
          <div className="flex items-center gap-2.5 flex-wrap text-xs">
            <Filter className="h-4 w-4 text-yellow-400 shrink-0" />
            <span className="font-semibold text-zinc-400 uppercase tracking-wider text-[11px]">
              Drill-Down Scope:
            </span>
            <span className="px-2 py-0.5 rounded bg-yellow-500/20 text-yellow-300 font-mono font-medium capitalize">
              {topologyFilter.type}
            </span>
            <span className="font-semibold text-white text-sm">
              {topologyFilter.name}
            </span>
            <span className="font-mono text-zinc-500 text-xs">
              ({topologyFilter.id})
            </span>
          </div>

          <button
            type="button"
            onClick={handleClearTopologyFilter}
            className="flex items-center gap-1.5 px-2.5 py-1 rounded-md bg-zinc-900/80 border border-zinc-700/80 text-xs text-zinc-300 hover:text-white hover:bg-zinc-800 transition-colors ml-4 shrink-0"
          >
            <X className="h-3.5 w-3.5" />
            <span>Reset to Fleet</span>
          </button>
        </div>
      )}

      {/* Main View */}
      {loading && vms.length === 0 ? (
        <div className="h-64 flex flex-col items-center justify-center gap-3 text-zinc-500">
          <Loader2 className="h-6 w-6 animate-spin text-yellow-600" />
          <span className="text-sm">Fetching virtual machine fleet...</span>
        </div>
      ) : view === "table" ? (
        <DataTable
          columns={vmColumns}
          data={vms}
          customSearch={searchBarControl}
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
            {searchBarControl}
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

export default function VmsPage() {
  return (
    <Suspense
      fallback={
        <div className="h-64 flex flex-col items-center justify-center gap-3 text-zinc-500">
          <Loader2 className="h-6 w-6 animate-spin text-yellow-600" />
          <span className="text-sm">Loading virtual machines...</span>
        </div>
      }
    >
      <VmsContent />
    </Suspense>
  );
}
