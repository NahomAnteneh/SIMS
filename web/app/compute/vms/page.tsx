"use client";

import React, { useState, useEffect, useCallback, Suspense, useMemo } from "react";
import { useSearchParams, useRouter } from "next/navigation";
import { Loader2, X } from "lucide-react";

import { vmColumns, type VirtualMachine } from "./columns";
import { DataTable } from "@/components/ui/data-table";
import { VMDetailSheet } from "@/components/vm/vm-detail-sheet";
import { simsApi } from "@/lib/api/client";
import { parseSearchQuery } from "@/lib/search/parser";
import { SearchFilter } from "@/lib/api/types";

interface ActiveFilterDisplay {
  id: string;
  label: string;
  onRemove: () => void;
}

function compileQueryFilter(q: string): SearchFilter {
  const trimmed = q.trim();
  const parsed = parseSearchQuery(trimmed);

  if (parsed.isStructured && parsed.filter) {
    if (parsed.freeText) {
      const freeMatches: SearchFilter[] = [
        { op: "CONTAINS", field: "name", value: parsed.freeText },
        { op: "CONTAINS", field: "guest_host_name", value: parsed.freeText },
        { op: "CONTAINS", field: "moref", value: parsed.freeText },
      ];
      if (/[0-9.:]/.test(parsed.freeText)) {
        freeMatches.push({
          op: "CIDR_CONTAINS",
          field: "guest_ip_address",
          value: parsed.freeText,
        });
      }
      return {
        op: "AND",
        operands: [parsed.filter, { op: "OR", operands: freeMatches }],
      };
    }
    return parsed.filter;
  }

  const textMatches: SearchFilter[] = [
    { op: "CONTAINS", field: "name", value: trimmed },
    { op: "CONTAINS", field: "guest_host_name", value: trimmed },
    { op: "CONTAINS", field: "moref", value: trimmed },
  ];
  if (/[0-9.:]/.test(trimmed)) {
    textMatches.push({
      op: "CIDR_CONTAINS",
      field: "guest_ip_address",
      value: trimmed,
    });
  }
  return textMatches.length === 1 ? textMatches[0] : { op: "OR", operands: textMatches };
}

function VmsContent() {
  const router = useRouter();
  const searchParams = useSearchParams();

  const [vms, setVms] = useState<VirtualMachine[]>([]);
  const [loading, setLoading] = useState(true);
  const [loadingMore, setLoadingMore] = useState(false);
  const [loadingAll, setLoadingAll] = useState(false);

  // Pagination state
  const [hasMore, setHasMore] = useState(false);
  const [nextCursor, setNextCursor] = useState<string | undefined>(undefined);
  const [selectedVMId, setSelectedVMId] = useState<string | null>(null);

  // Extract all query filters from URL
  const queryList = useMemo(() => {
    return searchParams ? searchParams.getAll("q").filter(Boolean) : [];
  }, [searchParams]);

  // Extract topology filters from URL
  const hostId = searchParams?.get("host_id");
  const hostName = searchParams?.get("host_name");
  const clusterId = searchParams?.get("cluster_id");
  const clusterName = searchParams?.get("cluster_name");
  const dsId = searchParams?.get("datastore_id");
  const dsName = searchParams?.get("datastore_name");
  const netId = searchParams?.get("network_id");
  const netName = searchParams?.get("network_name");
  const dcId = searchParams?.get("datacenter_id");
  const dcName = searchParams?.get("datacenter_name");
  const folderId = searchParams?.get("folder_id");
  const folderName = searchParams?.get("folder_name");
  const powerState = searchParams?.get("power_state");
  const vlanParam = searchParams?.get("vlan");

  const isFiltered = Boolean(
    queryList.length > 0 ||
    hostId ||
    clusterId ||
    dsId ||
    netId ||
    dcId ||
    folderId ||
    powerState ||
    vlanParam
  );

  // Build active filter chips for display
  const activeFilters = useMemo<ActiveFilterDisplay[]>(() => {
    const list: ActiveFilterDisplay[] = [];

    // Global search query filters
    queryList.forEach((q, idx) => {
      list.push({
        id: `q-${idx}-${q}`,
        label: q,
        onRemove: () => {
          const params = new URLSearchParams(searchParams ? searchParams.toString() : "");
          const remaining = params.getAll("q").filter((item) => item !== q);
          params.delete("q");
          remaining.forEach((item) => params.append("q", item));
          const qs = params.toString();
          router.push(`/compute/vms${qs ? `?${qs}` : ""}`);
        },
      });
    });

    if (hostId) {
      list.push({
        id: `host-${hostId}`,
        label: `host:${hostName || hostId}`,
        onRemove: () => {
          const params = new URLSearchParams(searchParams ? searchParams.toString() : "");
          params.delete("host_id");
          params.delete("host_name");
          const qs = params.toString();
          router.push(`/compute/vms${qs ? `?${qs}` : ""}`);
        },
      });
    }

    if (clusterId) {
      list.push({
        id: `cluster-${clusterId}`,
        label: `cluster:${clusterName || clusterId}`,
        onRemove: () => {
          const params = new URLSearchParams(searchParams ? searchParams.toString() : "");
          params.delete("cluster_id");
          params.delete("cluster_name");
          const qs = params.toString();
          router.push(`/compute/vms${qs ? `?${qs}` : ""}`);
        },
      });
    }

    if (dsId) {
      list.push({
        id: `ds-${dsId}`,
        label: `datastore:${dsName || dsId}`,
        onRemove: () => {
          const params = new URLSearchParams(searchParams ? searchParams.toString() : "");
          params.delete("datastore_id");
          params.delete("datastore_name");
          const qs = params.toString();
          router.push(`/compute/vms${qs ? `?${qs}` : ""}`);
        },
      });
    }

    if (netId) {
      list.push({
        id: `net-${netId}`,
        label: `network:${netName || netId}`,
        onRemove: () => {
          const params = new URLSearchParams(searchParams ? searchParams.toString() : "");
          params.delete("network_id");
          params.delete("network_name");
          const qs = params.toString();
          router.push(`/compute/vms${qs ? `?${qs}` : ""}`);
        },
      });
    }

    if (dcId) {
      list.push({
        id: `dc-${dcId}`,
        label: `dc:${dcName || dcId}`,
        onRemove: () => {
          const params = new URLSearchParams(searchParams ? searchParams.toString() : "");
          params.delete("datacenter_id");
          params.delete("datacenter_name");
          const qs = params.toString();
          router.push(`/compute/vms${qs ? `?${qs}` : ""}`);
        },
      });
    }

    if (folderId) {
      list.push({
        id: `folder-${folderId}`,
        label: `folder:${folderName || folderId}`,
        onRemove: () => {
          const params = new URLSearchParams(searchParams ? searchParams.toString() : "");
          params.delete("folder_id");
          params.delete("folder_name");
          const qs = params.toString();
          router.push(`/compute/vms${qs ? `?${qs}` : ""}`);
        },
      });
    }

    if (powerState) {
      list.push({
        id: `power-${powerState}`,
        label: `power:${powerState}`,
        onRemove: () => {
          const params = new URLSearchParams(searchParams ? searchParams.toString() : "");
          params.delete("power_state");
          const qs = params.toString();
          router.push(`/compute/vms${qs ? `?${qs}` : ""}`);
        },
      });
    }

    if (vlanParam) {
      list.push({
        id: `vlan-${vlanParam}`,
        label: `vlan:${vlanParam}`,
        onRemove: () => {
          const params = new URLSearchParams(searchParams ? searchParams.toString() : "");
          params.delete("vlan");
          const qs = params.toString();
          router.push(`/compute/vms${qs ? `?${qs}` : ""}`);
        },
      });
    }

    return list;
  }, [
    queryList,
    hostId,
    hostName,
    clusterId,
    clusterName,
    dsId,
    dsName,
    netId,
    netName,
    dcId,
    dcName,
    folderId,
    folderName,
    powerState,
    vlanParam,
    searchParams,
    router,
  ]);

  const handleClearAllFilters = useCallback(() => {
    router.push("/compute/vms");
    window.dispatchEvent(new Event("sims-clear-search"));
  }, [router]);

  // Main data loader
  const loadData = useCallback(
    async (cursor?: string, append = false) => {
      if (append) {
        setLoadingMore(true);
      } else {
        setLoading(true);
      }
      try {
        let items: VirtualMachine[] = [];
        let more = false;
        let nextCur: string | undefined = undefined;

        if (isFiltered) {
          const operands: SearchFilter[] = [];

          // 1. Process all query filters from top search bar
          for (const q of queryList) {
            operands.push(compileQueryFilter(q));
          }

          // 2. Process topology filters
          if (hostId) {
            operands.push({ op: "EQ", field: "host_id", value: hostId });
          }
          if (clusterId) {
            operands.push({ op: "EQ", field: "cluster_id", value: clusterId });
          }
          if (dsId) {
            operands.push({ op: "EQ", field: "datastore_id", value: dsId });
          }
          if (netId) {
            operands.push({ op: "EQ", field: "network_id", value: netId });
          }
          if (dcId) {
            operands.push({ op: "EQ", field: "datacenter_id", value: dcId });
          }
          if (folderId) {
            operands.push({ op: "EQ", field: "folder_id", value: folderId });
          }
          if (powerState) {
            operands.push({ op: "EQ", field: "power_state", value: powerState });
          }
          if (vlanParam) {
            operands.push({ op: "VLAN_OVERLAP", field: "vlan_ids", value: vlanParam });
          }

          const combinedFilter: SearchFilter =
            operands.length === 1 ? operands[0] : { op: "AND", operands };

          const res = await simsApi.structuredSearchVMs({
            filter: combinedFilter,
            limit: 100,
            cursor,
          });

          items = (res.items || []) as VirtualMachine[];
          more = res.hasMore;
          nextCur = res.nextCursor;
        } else {
          // Unfiltered fleet listing
          const res = await simsApi.listVMs({
            limit: 100,
            cursor,
          });

          items = (res.items || []) as VirtualMachine[];
          more = Boolean(
            res.pagination?.has_more ??
              (res as any).has_more ??
              (res as any).hasMore ??
              false
          );
          nextCur =
            res.pagination?.next_cursor ??
              (res as any).next_cursor ??
              (res as any).nextCursor;
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
    [
      isFiltered,
      queryList,
      hostId,
      clusterId,
      dsId,
      netId,
      dcId,
      folderId,
      powerState,
    ]
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

  return (
    <div className="w-full space-y-2">
      {/* Slim Active Filter Chips Bar (rendered ONLY when filters are active) */}
      {activeFilters.length > 0 && (
        <div className="flex items-center justify-between p-2 px-3 bg-[#0d0d0d] border border-zinc-800 text-xs font-mono gap-2 flex-wrap">
          <div className="flex items-center gap-2 flex-wrap">
            <span className="text-[10px] text-zinc-500 uppercase tracking-wider font-semibold">
              Filters:
            </span>
            {activeFilters.map((f) => (
              <span
                key={f.id}
                className="inline-flex items-center gap-1.5 px-2 py-0.5 bg-zinc-900 border border-zinc-700 text-zinc-200"
              >
                <span className="text-yellow-400 font-semibold">{f.label}</span>
                <button
                  type="button"
                  onClick={f.onRemove}
                  className="text-zinc-500 hover:text-rose-400 transition-colors p-0.5 ml-0.5"
                  title={`Remove filter ${f.label}`}
                >
                  <X className="h-3 w-3" />
                </button>
              </span>
            ))}
            <button
              type="button"
              onClick={handleClearAllFilters}
              className="text-zinc-400 hover:text-yellow-400 underline decoration-dotted text-xs ml-1"
            >
              Clear filters
            </button>
          </div>

          <div className="text-zinc-400 text-xs font-mono shrink-0">
            <span className="text-white font-bold">{vms.length}</span>
            {hasMore ? "+" : ""} matching
          </div>
        </div>
      )}

      {/* Main Table */}
      {loading && vms.length === 0 ? (
        <div className="h-64 flex flex-col items-center justify-center gap-3 text-zinc-500">
          <Loader2 className="h-6 w-6 animate-spin text-yellow-600" />
          <span className="text-sm font-mono">Fetching virtual machines...</span>
        </div>
      ) : (
        <DataTable
          columns={vmColumns}
          data={vms}
          onRowClick={(row) => setSelectedVMId(row.id)}
          pagination={{
            hasMore,
            loadingMore,
            onNext: handleShowMore,
            onLoadAll: hasMore ? handleLoadAll : undefined,
            loadingAll,
            entityName: isFiltered ? "matching virtual machines" : "virtual machines",
          }}
          tableContainerClassName="max-h-[calc(100vh-140px)] min-h-[400px]"
          hideHeader={true}
        />
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
          <span className="text-sm font-mono">Loading virtual machines...</span>
        </div>
      }
    >
      <VmsContent />
    </Suspense>
  );
}
