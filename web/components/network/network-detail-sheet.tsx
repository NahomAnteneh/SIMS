"use client";

import React, { useEffect, useState, useMemo } from "react";
import Link from "next/link";
import {
  X,
  Network,
  Monitor,
  ExternalLink,
  Layers,
  Cpu,
  Power,
  Search,
  Loader2,
  Copy,
  Check,
  Server,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { simsApi } from "@/lib/api/client";
import { VMListItem } from "@/lib/api/types";
import { formatVlanDisplay } from "@/lib/vlan";
import { type NetworkItem } from "@/app/infrastructure/columns";
import { VMDetailSheet } from "@/components/vm/vm-detail-sheet";

interface NetworkDetailSheetProps {
  network: NetworkItem | null;
  onClose: () => void;
}

export function NetworkDetailSheet({ network, onClose }: NetworkDetailSheetProps) {
  const [vms, setVms] = useState<VMListItem[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [searchFilter, setSearchFilter] = useState("");
  const [selectedVMId, setSelectedVMId] = useState<string | null>(null);
  const [copiedKey, setCopiedKey] = useState<string | null>(null);

  // Close on Escape key press
  useEffect(() => {
    if (!network) return;
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === "Escape" && !selectedVMId) {
        onClose();
      }
    };
    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, [network, onClose, selectedVMId]);

  // Fetch connected VMs when network opens
  useEffect(() => {
    if (!network) {
      setVms([]);
      return;
    }

    let isMounted = true;
    async function fetchVMs() {
      setLoading(true);
      setError(null);
      try {
        const res = await simsApi.listTopologyVMs("network", network!.id, { limit: 100 });
        if (isMounted) {
          setVms(res.items || []);
        }
      } catch (err: any) {
        if (isMounted) {
          setError(err.message || "Failed to retrieve connected workloads");
        }
      } finally {
        if (isMounted) {
          setLoading(false);
        }
      }
    }

    fetchVMs();
    return () => {
      isMounted = false;
    };
  }, [network]);

  const copyToClipboard = (text: string, key: string) => {
    navigator.clipboard.writeText(text);
    setCopiedKey(key);
    setTimeout(() => setCopiedKey(null), 2000);
  };

  const vlanDisplay = useMemo(() => {
    if (!network?.vlanIds) return "None";
    return formatVlanDisplay(network.vlanIds);
  }, [network?.vlanIds]);

  const filteredVMs = useMemo(() => {
    const q = searchFilter.trim().toLowerCase();
    if (!q) return vms;
    return vms.filter(
      (vm) =>
        vm.name.toLowerCase().includes(q) ||
        (vm.guest_ip_address && vm.guest_ip_address.toLowerCase().includes(q)) ||
        (vm.guest_host_name && vm.guest_host_name.toLowerCase().includes(q)) ||
        (vm.guest_os && vm.guest_os.toLowerCase().includes(q))
    );
  }, [vms, searchFilter]);

  if (!network) return null;

  return (
    <>
      {/* Backdrop */}
      <div
        className="fixed inset-0 bg-black/70 backdrop-blur-sm z-40 animate-in fade-in duration-200"
        onClick={onClose}
      />

      {/* Slide-over Panel */}
      <div className="fixed inset-y-0 right-0 z-50 w-full sm:w-[680px] md:w-[760px] bg-[#0a0a0a] border-l border-zinc-800 shadow-2xl flex flex-col animate-in slide-in-from-right duration-300 font-sans">
        {/* Header */}
        <div className="p-4 border-b border-zinc-800 flex items-center justify-between bg-[#0e0e0e]">
          <div className="flex items-center gap-3">
            <div className="p-2 bg-emerald-500/10 border border-emerald-500/30 text-emerald-400">
              <Network className="h-5 w-5" />
            </div>
            <div>
              <div className="flex items-center gap-2">
                <h2 className="text-base font-bold text-zinc-100 truncate max-w-[420px]">
                  {network.name}
                </h2>
                <button
                  onClick={() => copyToClipboard(network.name, "net-name")}
                  className="text-zinc-500 hover:text-white p-0.5"
                  title="Copy Network Name"
                >
                  {copiedKey === "net-name" ? (
                    <Check className="h-3 w-3 text-emerald-400" />
                  ) : (
                    <Copy className="h-3 w-3" />
                  )}
                </button>
              </div>
              <div className="flex items-center gap-2 mt-0.5 text-xs text-zinc-400 font-mono">
                <span className="text-zinc-500">#{network.moref || network.id.slice(0, 8)}</span>
                <span className="inline-flex items-center gap-1 px-1.5 py-0.2 text-[10px] font-bold border border-emerald-500/30 text-emerald-400 bg-emerald-500/10 uppercase">
                  {network.type || "Portgroup"}
                </span>
                {network.datacenterName && (
                  <span className="text-zinc-500">• {network.datacenterName}</span>
                )}
              </div>
            </div>
          </div>

          <div className="flex items-center gap-1.5">
            <Link
              href={`/compute/vms?network_id=${network.id}&network_name=${encodeURIComponent(network.name)}`}
              onClick={onClose}
              className="inline-flex items-center gap-1 px-2.5 py-1 text-xs font-mono font-medium rounded-none border border-zinc-700 bg-zinc-900 text-zinc-300 hover:text-yellow-400 hover:border-yellow-500/50 transition-colors"
              title="Open full filtered table in Compute"
            >
              <span>Explore in Fleet</span>
              <ExternalLink className="h-3 w-3" />
            </Link>

            <button
              onClick={onClose}
              aria-label="Close Network drawer"
              className="p-1.5 text-zinc-400 hover:text-zinc-100 hover:bg-zinc-800 transition-colors ml-1"
            >
              <X className="h-4 w-4" />
            </button>
          </div>
        </div>

        {/* Overview Stats Strip */}
        <div className="grid grid-cols-4 gap-2 p-4 pb-2 border-b border-zinc-850 bg-[#0c0c0c]">
          <div className="bg-[#121212] border border-zinc-800 p-2.5 flex flex-col items-center text-center">
            <Monitor className="h-4 w-4 text-yellow-500 mb-1" />
            <span className="text-sm font-bold text-zinc-100 font-mono">
              {network.vmCount || vms.length} VMs
            </span>
            <span className="text-[10px] text-zinc-500 uppercase tracking-wider font-mono">
              Workloads
            </span>
          </div>

          <div className="bg-[#121212] border border-zinc-800 p-2.5 flex flex-col items-center text-center">
            <Network className="h-4 w-4 text-amber-400 mb-1" />
            <span className="text-sm font-bold text-amber-400 font-mono truncate max-w-[130px]" title={vlanDisplay}>
              {vlanDisplay === "None" ? "Untagged" : `VLAN ${vlanDisplay}`}
            </span>
            <span className="text-[10px] text-zinc-500 uppercase tracking-wider font-mono">
              Allocation
            </span>
          </div>

          <div className="bg-[#121212] border border-zinc-800 p-2.5 flex flex-col items-center text-center">
            <Layers className="h-4 w-4 text-blue-400 mb-1" />
            <span className="text-xs font-bold text-zinc-200 font-mono truncate max-w-[130px]" title={network.switchName || "vSwitch"}>
              {network.switchName || "Standard vSwitch"}
            </span>
            <span className="text-[10px] text-zinc-500 uppercase tracking-wider font-mono">
              Switch / DVS
            </span>
          </div>

          <div className="bg-[#121212] border border-zinc-800 p-2.5 flex flex-col items-center text-center">
            <Server className="h-4 w-4 text-zinc-400 mb-1" />
            <span className="text-xs font-bold text-zinc-200 font-mono truncate max-w-[130px]" title={network.vcenterName || network.vcenterId}>
              {network.vcenterName || "vCenter"}
            </span>
            <span className="text-[10px] text-zinc-500 uppercase tracking-wider font-mono">
              vCenter
            </span>
          </div>
        </div>

        {/* Content Body: Connected Workloads */}
        <div className="flex-1 overflow-y-auto p-4 space-y-3 custom-scrollbar">
          <div className="flex items-center justify-between gap-3">
            <div className="flex items-center gap-2">
              <h3 className="text-xs font-mono uppercase font-bold text-zinc-300 tracking-wider">
                Attached Virtual Machines
              </h3>
              <span className="text-xs font-mono text-zinc-500">
                ({filteredVMs.length} {filteredVMs.length === 1 ? "workload" : "workloads"})
              </span>
            </div>

            {vlanDisplay !== "None" && (
              <Link
                href={`/compute/vms?vlan=${encodeURIComponent(vlanDisplay)}`}
                onClick={onClose}
                className="text-[11px] font-mono text-amber-400 hover:text-amber-300 underline decoration-amber-500/40"
              >
                View all VMs in VLAN {vlanDisplay} →
              </Link>
            )}
          </div>

          {/* Search inside workload list */}
          <div className="relative">
            <Search className="absolute left-2.5 top-2.5 h-3.5 w-3.5 text-zinc-500" />
            <Input
              type="text"
              placeholder="Filter workloads by name, IP, or OS..."
              value={searchFilter}
              onChange={(e) => setSearchFilter(e.target.value)}
              className="h-8 pl-8 text-xs font-mono bg-[#111] border-zinc-800 rounded-none text-zinc-200 placeholder:text-zinc-600 focus-visible:border-yellow-500"
            />
            {searchFilter && (
              <button
                onClick={() => setSearchFilter("")}
                className="absolute right-2.5 top-2 text-zinc-500 hover:text-white"
              >
                <X className="h-3.5 w-3.5" />
              </button>
            )}
          </div>

          {/* Workload List */}
          {loading ? (
            <div className="h-48 flex flex-col items-center justify-center gap-2.5 text-zinc-500">
              <Loader2 className="h-5 w-5 animate-spin text-yellow-500" />
              <span className="text-xs font-mono">Discovering connected workloads...</span>
            </div>
          ) : error ? (
            <div className="p-3 border border-rose-500/30 bg-rose-500/10 text-rose-400 text-xs font-mono">
              {error}
            </div>
          ) : filteredVMs.length === 0 ? (
            <div className="p-8 text-center text-xs font-mono text-zinc-500 border border-zinc-800 bg-[#0e0e0e]">
              {vms.length === 0
                ? "No virtual machines currently attached to this network or portgroup."
                : `No workloads matched filter "${searchFilter}".`}
            </div>
          ) : (
            <div className="space-y-1.5">
              {filteredVMs.map((vm) => {
                const isOn = vm.power_state === "poweredOn";
                const isSuspended = vm.power_state === "suspended";
                const vcpus = vm.cpu_cores;
                const ramGB = (vm.memory_mb / 1024).toFixed(1);

                return (
                  <div
                    key={vm.id}
                    onClick={() => setSelectedVMId(vm.id)}
                    className="p-3 bg-[#0e0e0e] border border-zinc-800/80 hover:border-yellow-600/50 hover:bg-zinc-900/30 cursor-pointer transition-colors group flex items-center justify-between gap-3"
                  >
                    <div className="flex items-center gap-3 min-w-0">
                      <div className="p-1.5 bg-zinc-900 border border-zinc-800 text-zinc-400 group-hover:text-yellow-400 transition-colors shrink-0">
                        <Monitor className="h-4 w-4" />
                      </div>

                      <div className="min-w-0">
                        <div className="flex items-center gap-2">
                          <span className="text-xs font-bold text-zinc-200 group-hover:text-white truncate">
                            {vm.name}
                          </span>
                          <span
                            className={`inline-flex items-center gap-1 px-1 py-0.2 text-[9px] font-mono uppercase font-semibold border ${
                              isOn
                                ? "text-emerald-400 bg-emerald-500/10 border-emerald-500/30"
                                : isSuspended
                                ? "text-amber-400 bg-amber-500/10 border-amber-500/30"
                                : "text-zinc-500 bg-zinc-900 border-zinc-800"
                            }`}
                          >
                            <Power className="h-2 w-2" />
                            {vm.power_state}
                          </span>
                        </div>

                        <div className="flex items-center gap-2 text-[11px] font-mono text-zinc-500 mt-0.5 truncate">
                          <span className="text-zinc-400">{vm.guest_ip_address || "No IP"}</span>
                          <span>•</span>
                          <span className="truncate max-w-[180px]">{vm.guest_os || vm.moref}</span>
                        </div>
                      </div>
                    </div>

                    <div className="text-right font-mono text-xs text-zinc-400 shrink-0">
                      <div>
                        <span>{vcpus} vCPU</span> • <span>{ramGB} GB</span>
                      </div>
                      <div className="text-[10px] text-zinc-500 mt-0.5">
                        {vm.folder_path ? vm.folder_path.split(".").pop() : "Root"}
                      </div>
                    </div>
                  </div>
                );
              })}
            </div>
          )}
        </div>

        {/* Footer */}
        <div className="p-3 border-t border-zinc-800 bg-[#0e0e0e] flex items-center justify-between text-xs font-mono text-zinc-500">
          <div>
            Total attached: <span className="text-zinc-300 font-bold">{vms.length}</span> workloads
          </div>
          <Button
            variant="outline"
            size="sm"
            onClick={onClose}
            className="h-7 px-3 text-xs rounded-none border-zinc-700 bg-zinc-900 text-zinc-300 hover:text-white"
          >
            Close
          </Button>
        </div>
      </div>

      {/* Nested VM Detail Sheet if a VM in the list is clicked */}
      {selectedVMId && (
        <VMDetailSheet
          vmId={selectedVMId}
          onClose={() => setSelectedVMId(null)}
        />
      )}
    </>
  );
}
