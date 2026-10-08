"use client";

import React, { useEffect, useState } from "react";
import Link from "next/link";
import { simsApi } from "@/lib/api/client";
import { VMDetailResponse } from "@/lib/api/types";
import { formatVlanDisplay } from "@/lib/vlan";
import {
  X,
  Server,
  Power,
  Cpu,
  Database,
  HardDrive,
  Network,
  Tag,
  FileText,
  Clock,
  Loader2,
  FolderTree,
  Copy,
  Check,
  Code,
  Layers,
  ExternalLink,
} from "lucide-react";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Button } from "@/components/ui/button";

interface VMDetailSheetProps {
  vmId: string | null;
  onClose: () => void;
}

export function VMDetailSheet({ vmId, onClose }: VMDetailSheetProps) {
  const [vm, setVM] = useState<VMDetailResponse | null>(null);
  const [hostInfo, setHostInfo] = useState<{ id: string; name: string } | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [copiedKey, setCopiedKey] = useState<string | null>(null);

  // Close on Escape key press
  useEffect(() => {
    if (!vmId) return;
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        onClose();
      }
    };
    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, [vmId, onClose]);

  useEffect(() => {
    if (!vmId) {
      setVM(null);
      return;
    }

    async function loadVM() {
      if (!vmId) return;
      setLoading(true);
      setError(null);
      try {
        const data = await simsApi.getVM(vmId);
        setVM(data);
      } catch (err: any) {
        setError(err?.message || "Failed to load VM details");
      } finally {
        setLoading(false);
      }
    }

    loadVM();
  }, [vmId]);

  useEffect(() => {
    if (!vm?.host_id) {
      setHostInfo(null);
      return;
    }
    let isCancelled = false;
    simsApi
      .getHost(vm.host_id)
      .then((h) => {
        if (!isCancelled) setHostInfo({ id: h.id, name: h.name });
      })
      .catch(() => {
        if (!isCancelled) setHostInfo(null);
      });
    return () => {
      isCancelled = true;
    };
  }, [vm?.host_id]);

  const copyToClipboard = (text: string, key: string) => {
    navigator.clipboard.writeText(text);
    setCopiedKey(key);
    setTimeout(() => {
      setCopiedKey((curr) => (curr === key ? null : curr));
    }, 2000);
  };

  if (!vmId) return null;

  const isOn = vm?.power_state === "poweredOn";
  const isSuspended = vm?.power_state === "suspended";

  // Accurate VM storage calculation from PropertyCollector summary
  const provisionedBytes = vm?.provisioned_storage_bytes || 0;
  const committedBytes = vm?.committed_storage_bytes || 0;
  const provisionedGB = (provisionedBytes / (1024 * 1024 * 1024)).toFixed(1);
  const committedGB = (committedBytes / (1024 * 1024 * 1024)).toFixed(1);

  return (
    <>
      {/* High-contrast solid dark backdrop - strictly NO background blur */}
      <div
        className="fixed inset-0 bg-black/80 z-40 transition-opacity"
        onClick={onClose}
        aria-hidden="true"
      />

      {/* Enterprise VM Inspector Drawer */}
      <div
        role="dialog"
        aria-modal="true"
        aria-labelledby="vm-detail-title"
        className="fixed inset-y-0 right-0 z-50 w-full max-w-2xl bg-[#0a0a0a] border-l border-zinc-800 shadow-2xl flex flex-col font-sans transition-all duration-300 rounded-none text-zinc-200"
      >
        {/* Drawer Header */}
        <div className="p-3.5 px-4 border-b border-zinc-800 bg-[#070707] flex items-center justify-between shrink-0">
          <div className="flex items-center gap-3 min-w-0">
            <div className="p-2 bg-yellow-500/10 text-yellow-500 border border-yellow-500/20 shrink-0">
              <Server className="h-4 w-4" />
            </div>
            <div className="min-w-0">
              <div className="flex items-center gap-2">
                <h2 id="vm-detail-title" className="text-sm font-bold text-zinc-100 truncate font-mono">
                  {vm?.name || "Loading instance..."}
                </h2>
                {vm && (
                  <button
                    onClick={() => copyToClipboard(vm.name, "name")}
                    title="Copy VM name"
                    className="text-zinc-500 hover:text-zinc-300 p-0.5"
                  >
                    {copiedKey === "name" ? (
                      <Check className="h-3 w-3 text-emerald-400" />
                    ) : (
                      <Copy className="h-3 w-3" />
                    )}
                  </button>
                )}
              </div>

              <div className="flex items-center gap-2 mt-0.5 text-xs text-zinc-400 font-mono">
                <span className="text-zinc-500">{vm?.moref}</span>
                {vm && (
                  <span
                    className={`inline-flex items-center gap-1 px-1.5 py-0.2 text-[10px] uppercase font-bold border ${
                      isOn
                        ? "text-emerald-400 bg-emerald-500/10 border-emerald-500/30"
                        : isSuspended
                        ? "text-amber-400 bg-amber-500/10 border-amber-500/30"
                        : "text-zinc-500 bg-zinc-900 border-zinc-800"
                    }`}
                  >
                    <Power className="h-2.5 w-2.5" />
                    {vm.power_state}
                  </span>
                )}
                {vm?.connection_state && (
                  <span className="text-[10px] text-zinc-500 uppercase">
                    ({vm.connection_state})
                  </span>
                )}
              </div>
            </div>
          </div>

          <div className="flex items-center gap-1.5">
            {vm && (
              <Button
                variant="outline"
                size="sm"
                onClick={() => copyToClipboard(JSON.stringify(vm, null, 2), "json")}
                className="h-7 px-2 text-[11px] font-mono rounded-none border-zinc-800 bg-zinc-900/50 text-zinc-400 hover:text-white"
                title="Copy formatted JSON payload"
              >
                {copiedKey === "json" ? (
                  <Check className="h-3 w-3 text-emerald-400 mr-1" />
                ) : (
                  <Code className="h-3 w-3 mr-1" />
                )}
                {copiedKey === "json" ? "Copied" : "JSON"}
              </Button>
            )}

            <button
              onClick={onClose}
              aria-label="Close VM detail dialog"
              className="p-1.5 text-zinc-400 hover:text-zinc-100 hover:bg-zinc-800 transition-colors"
            >
              <X className="h-4 w-4" />
            </button>
          </div>
        </div>

        {/* Drawer Content */}
        <div className="flex-1 overflow-y-auto p-4 custom-scrollbar">
          {loading ? (
            <div className="h-64 flex flex-col items-center justify-center gap-3 text-zinc-500">
              <Loader2 className="h-6 w-6 animate-spin text-yellow-500" />
              <span className="text-xs font-mono">Reading virtual machine telemetry...</span>
            </div>
          ) : error ? (
            <div className="p-3 border border-rose-500/30 bg-rose-500/10 text-rose-400 text-xs font-mono">
              {error}
            </div>
          ) : vm ? (
            <div className="space-y-4">
              {/* Quick Specs Strip */}
              <div className="grid grid-cols-4 gap-2">
                <div className="bg-[#0e0e0e] border border-zinc-800 p-2.5 flex flex-col items-center text-center">
                  <Cpu className="h-4 w-4 text-zinc-400 mb-1" />
                  <span className="text-sm font-bold text-zinc-100 font-mono">
                    {vm.cpu_cores} vCPU
                  </span>
                  <span className="text-[10px] text-zinc-500 uppercase tracking-wider font-mono">
                    Compute
                  </span>
                </div>

                <div className="bg-[#0e0e0e] border border-zinc-800 p-2.5 flex flex-col items-center text-center">
                  <Database className="h-4 w-4 text-zinc-400 mb-1" />
                  <span className="text-sm font-bold text-zinc-100 font-mono">
                    {(vm.memory_mb / 1024).toFixed(1)} GB
                  </span>
                  <span className="text-[10px] text-zinc-500 uppercase tracking-wider font-mono">
                    Memory
                  </span>
                </div>

                <div className="bg-[#0e0e0e] border border-zinc-800 p-2.5 flex flex-col items-center text-center">
                  <HardDrive className="h-4 w-4 text-cyan-400 mb-1" />
                  <span className="text-sm font-bold text-zinc-100 font-mono">
                    {provisionedBytes > 0 ? `${provisionedGB} GB` : `${vm.datastores?.length || 0} pools`}
                  </span>
                  <span className="text-[10px] text-zinc-500 uppercase tracking-wider font-mono">
                    {committedBytes > 0 ? `${committedGB} GB used` : "Storage"}
                  </span>
                </div>

                <div className="bg-[#0e0e0e] border border-zinc-800 p-2.5 flex flex-col items-center text-center">
                  <Network className="h-4 w-4 text-zinc-400 mb-1" />
                  <span className="text-sm font-bold text-zinc-100 font-mono">
                    {vm.network_interfaces?.length || 0} NICs
                  </span>
                  <span className="text-[10px] text-zinc-500 uppercase tracking-wider font-mono">
                    Interfaces
                  </span>
                </div>
              </div>

              {/* Tabs for Detailed Inspection */}
              <Tabs defaultValue="overview" className="w-full">
                <TabsList className="w-full justify-start bg-zinc-900 border border-zinc-800 p-0.5 rounded-none font-mono">
                  <TabsTrigger
                    value="overview"
                    className="rounded-none text-xs data-[state=active]:bg-zinc-800 data-[state=active]:text-yellow-400"
                  >
                    Overview
                  </TabsTrigger>
                  <TabsTrigger
                    value="disks"
                    className="rounded-none text-xs data-[state=active]:bg-zinc-800 data-[state=active]:text-yellow-400"
                  >
                    Storage ({vm.datastores?.length || 0})
                  </TabsTrigger>
                  <TabsTrigger
                    value="nics"
                    className="rounded-none text-xs data-[state=active]:bg-zinc-800 data-[state=active]:text-yellow-400"
                  >
                    Network ({vm.network_interfaces?.length || 0})
                  </TabsTrigger>
                  <TabsTrigger
                    value="tags"
                    className="rounded-none text-xs data-[state=active]:bg-zinc-800 data-[state=active]:text-yellow-400"
                  >
                    Tags & Meta
                  </TabsTrigger>
                  <TabsTrigger
                    value="raw"
                    className="rounded-none text-xs data-[state=active]:bg-zinc-800 data-[state=active]:text-yellow-400"
                  >
                    Raw Payload
                  </TabsTrigger>
                </TabsList>

                {/* Overview Tab */}
                <TabsContent value="overview" className="mt-3 space-y-3 text-xs">
                  <div className="border border-zinc-800 bg-[#0e0e0e] p-3 space-y-2">
                    <div className="text-[10px] font-mono uppercase text-zinc-500 font-bold tracking-wider mb-1">
                      Guest OS & Workload Identification
                    </div>

                    <div className="flex justify-between py-1.5 border-b border-zinc-850">
                      <span className="text-zinc-500">Primary Guest IP</span>
                      <div className="flex items-center gap-1.5 font-mono text-zinc-200">
                        <span>{vm.guest_ip_address || "None"}</span>
                        {vm.guest_ip_address && (
                          <button
                            onClick={() =>
                              copyToClipboard(vm.guest_ip_address!, "ip")
                            }
                            className="text-zinc-500 hover:text-white p-0.5"
                            title="Copy IP"
                          >
                            {copiedKey === "ip" ? (
                              <Check className="h-2.5 w-2.5 text-emerald-400" />
                            ) : (
                              <Copy className="h-2.5 w-2.5" />
                            )}
                          </button>
                        )}
                      </div>
                    </div>

                    <div className="flex justify-between py-1.5 border-b border-zinc-850">
                      <span className="text-zinc-500">Guest OS</span>
                      <span className="text-zinc-200 truncate max-w-[320px]">
                        {vm.guest_os || "Unknown"}
                      </span>
                    </div>

                    <div className="flex justify-between py-1.5 border-b border-zinc-850">
                      <span className="text-zinc-500">Guest Hostname</span>
                      <span className="text-zinc-200 font-mono">
                        {vm.guest_host_name || "N/A"}
                      </span>
                    </div>

                    <div className="flex justify-between py-1.5 border-b border-zinc-850">
                      <span className="text-zinc-500">Folder Hierarchy</span>
                      <span className="text-zinc-300 font-mono flex items-center gap-1 truncate max-w-[320px]">
                        <FolderTree className="h-3 w-3 text-yellow-500" />
                        {vm.folder_path || "Root"}
                      </span>
                    </div>

                    <div className="flex justify-between py-1.5 border-b border-zinc-850">
                      <span className="text-zinc-500">Hypervisor Host</span>
                      {vm.host_id ? (
                        <div className="flex items-center gap-1.5 font-mono text-zinc-200 truncate max-w-[320px]">
                          <Server className="h-3 w-3 text-yellow-500 shrink-0" />
                          <Link
                            href={`/compute/vms?host_id=${vm.host_id}&host_name=${encodeURIComponent(hostInfo?.name || vm.host_id)}`}
                            onClick={onClose}
                            className="text-yellow-400 hover:text-yellow-300 hover:underline truncate"
                            title="Filter virtual machines on this ESXi host"
                          >
                            {hostInfo?.name || vm.host_id}
                          </Link>
                        </div>
                      ) : (
                        <span className="text-zinc-500 font-mono">Unassigned</span>
                      )}
                    </div>

                    <div className="flex justify-between py-1.5 border-b border-zinc-850">
                      <span className="text-zinc-500">Instance UUID</span>
                      <div className="flex items-center gap-1.5 font-mono text-zinc-400 truncate max-w-[320px]">
                        <span className="truncate">{vm.instance_uuid}</span>
                        <button
                          onClick={() =>
                            copyToClipboard(vm.instance_uuid, "inst_uuid")
                          }
                          className="text-zinc-500 hover:text-white p-0.5"
                          title="Copy Instance UUID"
                        >
                          {copiedKey === "inst_uuid" ? (
                            <Check className="h-2.5 w-2.5 text-emerald-400" />
                          ) : (
                            <Copy className="h-2.5 w-2.5" />
                          )}
                        </button>
                      </div>
                    </div>

                    {vm.bios_uuid && (
                      <div className="flex justify-between py-1.5 border-b border-zinc-850">
                        <span className="text-zinc-500">BIOS UUID</span>
                        <span className="font-mono text-zinc-400 truncate max-w-[320px]">
                          {vm.bios_uuid}
                        </span>
                      </div>
                    )}

                    <div className="flex justify-between py-1.5 border-b border-zinc-850">
                      <span className="text-zinc-500">vCenter Endpoint ID</span>
                      <span className="font-mono text-zinc-400 truncate max-w-[320px]">
                        {vm.vcenter_id}
                      </span>
                    </div>

                    <div className="flex justify-between py-1.5">
                      <span className="text-zinc-500">First Ingested</span>
                      <span className="text-zinc-400 font-mono flex items-center gap-1">
                        <Clock className="h-3 w-3" />
                        {new Date(vm.created_at).toLocaleString()}
                      </span>
                    </div>
                  </div>

                  {vm.annotation && (
                    <div className="border border-zinc-800 bg-[#0e0e0e] p-3">
                      <div className="text-[10px] font-semibold text-zinc-400 mb-1 flex items-center gap-1.5 font-mono uppercase tracking-wider">
                        <FileText className="h-3.5 w-3.5 text-yellow-500" />
                        Operator Notes & Annotation
                      </div>
                      <p className="text-zinc-300 font-mono text-[11px] whitespace-pre-wrap leading-relaxed">
                        {vm.annotation}
                      </p>
                    </div>
                  )}
                </TabsContent>

                {/* Disks & Storage Tab */}
                <TabsContent value="disks" className="mt-3 space-y-3">
                  <div className="p-3 border border-zinc-800 bg-[#121212] flex items-center justify-between">
                    <div>
                      <div className="text-xs font-semibold text-zinc-100 font-mono">Virtual Disk Footprint</div>
                      <div className="text-[10px] text-zinc-400 font-mono mt-0.5">
                        Provisioned: <span className="text-cyan-400 font-bold">{provisionedGB} GB</span> • Committed (Used): <span className="text-emerald-400 font-bold">{committedGB} GB</span>
                      </div>
                    </div>
                    <div className="text-right font-mono text-[10px] text-zinc-500">
                      {vm.datastores?.length || 0} Storage Pool{vm.datastores?.length === 1 ? "" : "s"}
                    </div>
                  </div>
                  {vm.datastores && vm.datastores.length > 0 ? (
                    vm.datastores.map((ds) => {
                      const capBytes = ds.capacity_bytes || 0;
                      const freeBytes = ds.free_bytes || 0;
                      const usedBytes = Math.max(0, capBytes - freeBytes);
                      const hasCap = capBytes > 0;
                      const usedPercent = hasCap ? Math.round((usedBytes / capBytes) * 100) : 0;

                      const formatBytes = (b: number) => {
                        if (b <= 0) return "0 GB";
                        const gb = b / (1024 * 1024 * 1024);
                        if (gb >= 1000) return `${(gb / 1024).toFixed(1)} TB`;
                        return `${gb.toFixed(1)} GB`;
                      };

                      return (
                        <div
                          key={ds.id}
                          className="p-3 border border-zinc-800 bg-[#0e0e0e] space-y-2"
                        >
                          <div className="flex items-center justify-between">
                            <div className="flex items-center gap-2.5">
                              <HardDrive className="h-4 w-4 text-cyan-400" />
                              <div>
                                <div className="text-xs font-semibold text-zinc-100 font-mono">
                                  {ds.name}
                                </div>
                                <div className="text-[10px] text-zinc-500 font-mono">
                                  {ds.type || "VMFS"} • {ds.moref}
                                </div>
                              </div>
                            </div>
                            <div className="text-right font-mono">
                              <div className="text-xs font-bold text-zinc-200">
                                {formatBytes(capBytes)}
                              </div>
                              <div className="text-[10px] text-zinc-500">
                                {formatBytes(freeBytes)} free ({100 - usedPercent}%)
                              </div>
                            </div>
                          </div>

                          {/* Storage Utilization Bar */}
                          {hasCap && (
                            <div className="space-y-1 pt-1">
                              <div className="h-1.5 w-full bg-zinc-900 overflow-hidden">
                                <div
                                  className={`h-full ${
                                    usedPercent > 85
                                      ? "bg-rose-500"
                                      : usedPercent > 70
                                      ? "bg-amber-500"
                                      : "bg-cyan-500"
                                  }`}
                                  style={{ width: `${usedPercent}%` }}
                                />
                              </div>
                              <div className="flex justify-between text-[10px] font-mono text-zinc-500">
                                <span>Used: {formatBytes(usedBytes)} ({usedPercent}%)</span>
                                <span>Total: {formatBytes(capBytes)}</span>
                              </div>
                            </div>
                          )}
                        </div>
                      );
                    })
                  ) : (
                    <div className="p-8 text-center text-xs font-mono text-zinc-500 border border-zinc-800 bg-[#0e0e0e]">
                      No attached datastores reported by property collector.
                    </div>
                  )}
                </TabsContent>

                {/* Network Interfaces Tab */}
                <TabsContent value="nics" className="mt-3 space-y-2">
                  {vm.network_interfaces && vm.network_interfaces.length > 0 ? (
                    vm.network_interfaces.map((nic) => {
                      const netId = nic.portgroup_id || nic.network_id;
                      const netName = nic.network_name || "Standard Network";

                      return (
                        <div
                          key={nic.id}
                          className="p-3 border border-zinc-800 bg-[#0e0e0e] space-y-2"
                        >
                          <div className="flex items-center justify-between">
                            <div className="flex items-center gap-2.5">
                              <Network className="h-4 w-4 text-emerald-400 shrink-0" />
                              <div>
                                <div className="text-xs font-mono font-bold text-zinc-100 flex items-center gap-1.5">
                                  <span>{netName}</span>
                                  {nic.vlan_ids && formatVlanDisplay(nic.vlan_ids) !== "None" && (
                                    <span className="text-[10px] font-mono font-semibold text-amber-400 bg-amber-400/10 px-1.5 py-0.2 border border-amber-500/20">
                                      VLAN {formatVlanDisplay(nic.vlan_ids)}
                                    </span>
                                  )}
                                </div>
                                <div className="text-[10px] text-zinc-500 font-mono flex items-center gap-2 mt-0.5">
                                  <span>Device Key: {nic.device_key}</span>
                                  {netId && <span>• ID: {netId.slice(0, 8)}</span>}
                                </div>
                              </div>
                            </div>

                            <div className="text-right font-mono">
                              <div className="text-xs font-bold text-zinc-200">
                                {nic.ip_address || "No Guest IP"}
                              </div>
                              <div className="text-[10px] text-zinc-500">
                                Assigned IP
                              </div>
                            </div>
                          </div>

                          <div className="pt-1.5 border-t border-zinc-850 flex items-center justify-between text-[11px] font-mono">
                            <span className="text-zinc-500">Hardware MAC</span>
                            <div className="flex items-center gap-1.5 text-zinc-300">
                              <span>{nic.mac_address}</span>
                              <button
                                onClick={() => copyToClipboard(nic.mac_address, nic.id)}
                                className="text-zinc-500 hover:text-white p-0.5"
                                title="Copy MAC address"
                              >
                                {copiedKey === nic.id ? (
                                  <Check className="h-2.5 w-2.5 text-emerald-400" />
                                ) : (
                                  <Copy className="h-2.5 w-2.5" />
                                )}
                              </button>
                            </div>
                          </div>
                        </div>
                      );
                    })
                  ) : (
                    <div className="p-8 text-center text-xs font-mono text-zinc-500 border border-zinc-800 bg-[#0e0e0e]">
                      No network interfaces reported by property collector.
                    </div>
                  )}
                </TabsContent>

                {/* Tags & Metadata Tab */}
                <TabsContent value="tags" className="mt-3 space-y-3">
                  <div>
                    <h4 className="text-[10px] font-mono uppercase tracking-wider text-zinc-400 mb-1.5 flex items-center gap-1.5 font-bold">
                      <Tag className="h-3 w-3 text-yellow-500" />
                      vSphere Tags ({vm.tags?.length || 0})
                    </h4>
                    {vm.tags && vm.tags.length > 0 ? (
                      <div className="flex flex-wrap gap-1.5">
                        {vm.tags.map((t, idx) => (
                          <span
                            key={idx}
                            className="px-2 py-0.5 bg-zinc-900 border border-zinc-800 text-xs font-mono text-zinc-200"
                          >
                            <span className="text-zinc-500">{t.category_name}:</span>{" "}
                            {t.tag_name}
                          </span>
                        ))}
                      </div>
                    ) : (
                      <p className="text-xs font-mono text-zinc-500">No vSphere tags assigned.</p>
                    )}
                  </div>

                  <div>
                    <h4 className="text-[10px] font-mono uppercase tracking-wider text-zinc-400 mb-1.5 flex items-center gap-1.5 font-bold">
                      <FileText className="h-3 w-3 text-yellow-500" />
                      Custom Attributes ({vm.custom_attributes?.length || 0})
                    </h4>
                    {vm.custom_attributes && vm.custom_attributes.length > 0 ? (
                      <div className="space-y-1">
                        {vm.custom_attributes.map((ca, idx) => (
                          <div
                            key={idx}
                            className="p-2 border border-zinc-800 bg-[#0e0e0e] flex justify-between text-xs font-mono"
                          >
                            <span className="text-zinc-400">{ca.name}</span>
                            <span className="text-zinc-200">{ca.value}</span>
                          </div>
                        ))}
                      </div>
                    ) : (
                      <p className="text-xs font-mono text-zinc-500">
                        No custom attributes defined.
                      </p>
                    )}
                  </div>
                </TabsContent>

                {/* Raw JSON Inspector Tab */}
                <TabsContent value="raw" className="mt-3 space-y-2">
                  <div className="flex items-center justify-between pb-1">
                    <span className="text-[10px] font-mono uppercase text-zinc-500">
                      Raw Query API Response
                    </span>
                    <button
                      onClick={() =>
                        copyToClipboard(JSON.stringify(vm, null, 2), "raw_json")
                      }
                      className="text-xs font-mono text-yellow-400 hover:underline flex items-center gap-1"
                    >
                      {copiedKey === "raw_json" ? "Copied to clipboard!" : "Copy JSON"}
                    </button>
                  </div>
                  <pre className="p-3 bg-black border border-zinc-800 text-[11px] font-mono text-zinc-300 overflow-x-auto max-h-96 whitespace-pre custom-scrollbar">
                    {JSON.stringify(vm, null, 2)}
                  </pre>
                </TabsContent>
              </Tabs>
            </div>
          ) : null}
        </div>
      </div>
    </>
  );
}
