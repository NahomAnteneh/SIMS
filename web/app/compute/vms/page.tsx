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
  Filter,
} from "lucide-react";

import { vmColumns, type VirtualMachine } from "./columns";
import { DataTable } from "@/components/ui/data-table";
import { VmCard } from "./card";
import { VMDetailSheet } from "@/components/vm/vm-detail-sheet";
import { simsApi } from "@/lib/api/client";
import { Button } from "@/components/ui/button";
import { parseSearchQuery } from "@/lib/search/parser";
import { SearchFilter } from "@/lib/api/types";

interface TopologyFilter {
  type: "datacenter" | "cluster" | "host" | "datastore" | "network" | "folder";
  id: string;
  name: string;
}

function VmsContent() {
  const router = useRouter();
  const searchParams = useSearchParams();

  const [view, setView] = useState<"table" | "card">("table");
  const [vms, setVms] = useState<VirtualMachine[]>([]);
  const [loading, setLoading] = useState(true);
  const [loadingMore, setLoadingMore] = useState(false);
  const [loadingAll, setLoadingAll] = useState(false);
  const [totalFleetCount, setTotalFleetCount] = useState<number | null>(null);
  const [limit, setLimit] = useState(100);
  const [searchTerm, setSearchTerm] = useState("");
  const [debouncedSearch, setDebouncedSearch] = useState("");
  const [powerState, setPowerState] = useState<string>("all");

  const [hasMore, setHasMore] = useState(false);
  const [nextCursor, setNextCursor] = useState<string | undefined>(undefined);
  const [selectedVMId, setSelectedVMId] = useState<string | null>(null);

  const [topologyFilter, setTopologyFilter] = useState<TopologyFilter | null>(null);

  // Fetch total fleet count on mount
  useEffect(() => {
    async function fetchTotalCount() {
      try {
        const facetsRes = await simsApi.getVMFacets("power_state");
        const buckets = facetsRes.facets?.power_state || [];
        const total = buckets.reduce((sum, b) => sum + b.count, 0);
        if (total > 0) setTotalFleetCount(total);
      } catch {
        // ignore facets error
      }
    }
    fetchTotalCount();
  }, []);

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

  const loadData = useCallback(
    async (cursor?: string, append = false) => {
      if (append) {
        setLoadingMore(true);
      } else {
        setLoading(true);
      }
      try {
        const trimmed = debouncedSearch.trim();
        const hasPower = powerState !== "all";

        let items: VirtualMachine[] = [];
        let more = false;
        let nextCur: string | undefined = undefined;

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
            items = (res.items || []) as VirtualMachine[];
            more = res.hasMore;
            nextCur = res.nextCursor;
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
              items = (res.items || []) as VirtualMachine[];
              more = res.hasMore;
              nextCur = res.nextCursor;
            } else {
              const res = await simsApi.quickSearchVMs(trimmed, limit, cursor);
              items = (res.items || []) as VirtualMachine[];
              more = res.hasMore;
              nextCur = res.nextCursor;
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
            items = (res.items || []) as VirtualMachine[];
            more = res.hasMore;
            nextCur = res.nextCursor;
          } else {
            const res = await simsApi.listTopologyVMs(topologyFilter.type, topologyFilter.id, {
              limit,
              cursor,
            });
            items = (res.items || []) as VirtualMachine[];
            more = res.pagination.has_more;
            nextCur = res.pagination.next_cursor;
          }
        } else if (hasPower) {
          const res = await simsApi.searchVMs({
            filter: { op: "EQ", field: "power_state", value: powerState },
            limit,
            cursor,
          });
          items = (res.items || []) as VirtualMachine[];
          more = res.hasMore;
          nextCur = res.nextCursor;
        } else {
          const res = await simsApi.listVMs({ limit, cursor });
          items = (res.items || []) as VirtualMachine[];
          more = res.pagination.has_more;
          nextCur = res.pagination.next_cursor;
        }

        setVms((prev) => {
          if (!append) return items;
          const existingIds = new Set(prev.map((vm) => vm.id));
          const newItems = items.filter((vm) => !existingIds.has(vm.id));
          return [...prev, ...newItems];
        });
        setHasMore(more);
        setNextCursor(nextCur);
        return { more, nextCur };
      } catch (err) {
        console.error("Failed to load virtual machines", err);
        return { more: false, nextCur: undefined };
      } finally {
        setLoading(false);
        setLoadingMore(false);
      }
    },
    [limit, debouncedSearch, powerState, topologyFilter]
  );

  useEffect(() => {
    loadData(undefined, false);
  }, [loadData]);

  const handleShowMore = useCallback(() => {
    if (!nextCursor || loading || loadingMore || loadingAll) return;
    loadData(nextCursor, true);
  }, [nextCursor, loading, loadingMore, loadingAll, loadData]);

  const handleLoadAll = async () => {
    if (!nextCursor || loading || loadingMore || loadingAll) return;
    setLoadingAll(true);
    let cur: string | undefined = nextCursor;
    try {
      while (cur) {
        const result = await loadData(cur, true);
        if (!result.more || !result.nextCur) break;
        cur = result.nextCur;
      }
    } catch (err) {
      console.error("Failed to load all VMs", err);
    } finally {
      setLoadingAll(false);
    }
  };

  const handleClearTopologyFilter = () => {
    setTopologyFilter(null);
    router.replace("/compute/vms");
  };

  const activeQueryBadge = debouncedSearch ? (
    <div className="flex items-center gap-1.5 px-2.5 py-1 bg-yellow-500/10 border border-yellow-600/30 text-yellow-300 text-xs font-mono">
      <Search className="h-3 w-3 text-yellow-400" />
      <span className="text-zinc-400">Query:</span>
      <span className="font-semibold text-white">{debouncedSearch}</span>
      <button
        type="button"
        onClick={() => {
          setSearchTerm("");
          setDebouncedSearch("");
          const url = new URL(window.location.href);
          url.searchParams.delete("q");
          router.replace(url.pathname + (url.search ? url.search : ""));
        }}
        className="ml-1 text-zinc-400 hover:text-white"
        title="Clear query"
      >
        <X className="h-3 w-3" />
      </button>
    </div>
  ) : null;

  const filterControl = (
    <div className="flex items-center gap-2 shrink-0">
      <select
        value={powerState}
        onChange={(e) => setPowerState(e.target.value)}
        className="h-8 border border-zinc-800 bg-zinc-900/80 px-2.5 py-1 text-xs text-zinc-300 focus:outline-none focus:border-zinc-700 rounded-none font-mono"
      >
        <option value="all">All States</option>
        <option value="poweredOn">poweredOn</option>
        <option value="poweredOff">poweredOff</option>
        <option value="suspended">suspended</option>
      </select>
      {(searchTerm || powerState !== "all" || topologyFilter) && (
        <button
          onClick={() => {
            setSearchTerm("");
            setDebouncedSearch("");
            setPowerState("all");
            if (topologyFilter) handleClearTopologyFilter();
          }}
          className="text-xs text-zinc-400 hover:text-yellow-400 font-mono underline decoration-dotted whitespace-nowrap"
        >
          Reset
        </button>
      )}
    </div>
  );

  return (
    <div className="space-y-4 w-full">
      {/* Page Header */}
      <div className="flex items-center justify-between pb-2 border-b border-zinc-800/80">
        <div className="flex items-center gap-3">
          <h1 className="text-xl font-bold tracking-tight text-zinc-100 flex items-center gap-2">
            Virtual Machines
            <span className="text-xs font-mono font-normal text-zinc-500">
              ({vms.length}{totalFleetCount ? ` of ${totalFleetCount}` : ""} loaded)
            </span>
            {topologyFilter && (
              <span className="text-xs font-mono font-normal px-2 py-0.5 bg-yellow-500/10 border border-yellow-500/30 text-yellow-400">
                scope: {topologyFilter.type}
              </span>
            )}
          </h1>
        </div>

        <div className="flex items-center gap-2">
          <Button
            variant="outline"
            size="sm"
            onClick={() => loadData(undefined, false)}
            disabled={loading}
            className="border-zinc-800 bg-zinc-900/50 text-zinc-300 hover:text-white hover:bg-zinc-800 gap-1.5 h-8 text-xs rounded-none"
          >
            <RefreshCw className={`h-3.5 w-3.5 ${loading ? "animate-spin" : ""}`} />
            Refresh
          </Button>

          {/* View Toggle */}
          <div className="flex items-center border border-zinc-800 bg-zinc-900/50 p-0.5">
            <button
              type="button"
              onClick={() => setView("table")}
              className={`flex items-center gap-1.5 px-2.5 py-1 text-xs font-medium transition-colors ${
                view === "table"
                  ? "bg-zinc-800 text-zinc-100"
                  : "text-zinc-400 hover:text-zinc-200"
              }`}
            >
              <LayoutList className="h-3.5 w-3.5" />
              Table
            </button>
            <button
              type="button"
              onClick={() => setView("card")}
              className={`flex items-center gap-1.5 px-2.5 py-1 text-xs font-medium transition-colors ${
                view === "card"
                  ? "bg-zinc-800 text-zinc-100"
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
        <div className="flex items-center justify-between p-2.5 px-3 border border-yellow-600/50 bg-yellow-950/20 text-yellow-200">
          <div className="flex items-center gap-2 flex-wrap text-xs font-mono">
            <Filter className="h-3.5 w-3.5 text-yellow-400 shrink-0" />
            <span className="font-semibold text-zinc-400 uppercase tracking-wider text-[11px]">
              Drill-Down Scope:
            </span>
            <span className="px-1.5 py-0.2 bg-yellow-500/20 text-yellow-300 font-bold capitalize">
              {topologyFilter.type}
            </span>
            <span className="font-semibold text-white">
              {topologyFilter.name}
            </span>
            <span className="text-zinc-500 text-xs">
              ({topologyFilter.id})
            </span>
          </div>

          <button
            type="button"
            onClick={handleClearTopologyFilter}
            className="flex items-center gap-1 px-2 py-0.5 bg-zinc-900 border border-zinc-700 text-xs font-mono text-zinc-300 hover:text-white hover:bg-zinc-800 transition-colors shrink-0"
          >
            <X className="h-3 w-3" />
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
          customSearch={activeQueryBadge}
          filterComponent={filterControl}
          onRowClick={(row) => setSelectedVMId(row.id)}
          pagination={{
            hasMore,
            loadingMore,
            onNext: handleShowMore,
            total: totalFleetCount ?? undefined,
            onLoadAll: hasMore ? handleLoadAll : undefined,
            loadingAll,
            entityName: "virtual machines",
          }}
          tableContainerClassName="max-h-[calc(100vh-230px)] min-h-[420px]"
        />
      ) : (
        <div className="space-y-4">
          <div className="flex items-center justify-between p-2.5 border border-zinc-800 bg-[#0c0c0c] gap-3">
            <div className="flex-1">{activeQueryBadge}</div>
            {filterControl}
          </div>
          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
            {vms.map((vm) => (
              <VmCard key={vm.id} vm={vm} onClick={() => setSelectedVMId(vm.id)} />
            ))}
          </div>
          {hasMore && (
            <div className="flex items-center justify-center gap-3 pt-2">
              <Button
                variant="outline"
                size="sm"
                onClick={handleShowMore}
                disabled={loadingMore || loadingAll}
                className="h-8 px-5 text-xs font-mono rounded-none border-zinc-800 bg-zinc-900/60 text-yellow-400 hover:bg-zinc-800 hover:border-yellow-600 gap-1.5"
              >
                {loadingMore && <Loader2 className="h-3 w-3 animate-spin" />}
                {loadingMore ? "Loading..." : "Show more"}
              </Button>
              <Button
                variant="ghost"
                size="sm"
                onClick={handleLoadAll}
                disabled={loadingMore || loadingAll}
                className="h-8 px-4 text-xs font-mono rounded-none text-zinc-400 hover:text-white"
              >
                {loadingAll ? "Loading all..." : "Load all remaining"}
              </Button>
            </div>
          )}
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
