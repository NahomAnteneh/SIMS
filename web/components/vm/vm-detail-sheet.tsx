"use client";

import React, { useEffect, useState } from "react";
import { simsApi } from "@/lib/api/client";
import { VMDetailResponse } from "@/lib/api/types";
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
} from "lucide-react";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";

interface VMDetailSheetProps {
  vmId: string | null;
  onClose: () => void;
}

export function VMDetailSheet({ vmId, onClose }: VMDetailSheetProps) {
  const [vm, setVM] = useState<VMDetailResponse | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

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

  if (!vmId) return null;

  const isOn = vm?.power_state === "poweredOn";
  const isSuspended = vm?.power_state === "suspended";

  return (
    <>
      <div
        className="fixed inset-0 bg-black/60 backdrop-blur-sm z-40 transition-opacity"
        onClick={onClose}
      />
      <div className="fixed inset-y-0 right-0 z-50 w-full max-w-xl bg-[#0e0e0e] border-l border-zinc-800 shadow-2xl flex flex-col font-sans transition-all duration-300">
      {/* Drawer Header */}
      <div className="p-5 border-b border-zinc-800/80 flex items-center justify-between bg-zinc-950/60">
        <div className="flex items-center gap-3 min-w-0">
          <div className="p-2 rounded-lg bg-yellow-600/10 text-yellow-500 border border-yellow-600/20 shrink-0">
            <Server className="h-5 w-5" />
          </div>
          <div className="min-w-0">
            <h2 className="text-base font-bold text-zinc-100 truncate">
              {vm?.name || "Loading..."}
            </h2>
            <div className="flex items-center gap-2 mt-0.5 text-xs text-zinc-400">
              <span className="font-mono text-zinc-500">{vm?.moref}</span>
              {vm && (
                <span
                  className={`inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-[10px] font-medium ${
                    isOn
                      ? "text-emerald-400 bg-emerald-400/10"
                      : isSuspended
                      ? "text-amber-400 bg-amber-400/10"
                      : "text-zinc-500 bg-zinc-800"
                  }`}
                >
                  <Power className="h-2.5 w-2.5" />
                  {vm.power_state}
                </span>
              )}
            </div>
          </div>
        </div>

        <button
          onClick={onClose}
          className="p-1.5 rounded-md text-zinc-400 hover:text-zinc-100 hover:bg-zinc-800 transition-colors"
        >
          <X className="h-5 w-5" />
        </button>
      </div>

      {/* Drawer Content */}
      <div className="flex-1 overflow-y-auto p-5 custom-scrollbar">
        {loading ? (
          <div className="h-64 flex flex-col items-center justify-center gap-3 text-zinc-500">
            <Loader2 className="h-6 w-6 animate-spin text-yellow-500" />
            <span className="text-sm">Loading virtual machine details...</span>
          </div>
        ) : error ? (
          <div className="p-4 rounded-lg border border-rose-500/20 bg-rose-500/10 text-rose-400 text-sm">
            {error}
          </div>
        ) : vm ? (
          <div className="space-y-6">
            {/* Quick Specs Badges */}
            <div className="grid grid-cols-3 gap-3">
              <div className="bg-zinc-900/60 border border-zinc-800 rounded-xl p-3 flex flex-col items-center text-center">
                <Cpu className="h-4 w-4 text-zinc-400 mb-1" />
                <span className="text-sm font-bold text-zinc-100">{vm.cpu_cores} vCPU</span>
                <span className="text-[10px] text-zinc-500">Compute Cores</span>
              </div>
              <div className="bg-zinc-900/60 border border-zinc-800 rounded-xl p-3 flex flex-col items-center text-center">
                <Database className="h-4 w-4 text-zinc-400 mb-1" />
                <span className="text-sm font-bold text-zinc-100">
                  {(vm.memory_mb / 1024).toFixed(1)} GB
                </span>
                <span className="text-[10px] text-zinc-500">RAM Allocation</span>
              </div>
              <div className="bg-zinc-900/60 border border-zinc-800 rounded-xl p-3 flex flex-col items-center text-center">
                <HardDrive className="h-4 w-4 text-zinc-400 mb-1" />
                <span className="text-sm font-bold text-zinc-100">
                  {vm.datastores?.length || 0} Disks
                </span>
                <span className="text-[10px] text-zinc-500">Datastores</span>
              </div>
            </div>

            {/* Tabs for Detailed Inspection */}
            <Tabs defaultValue="overview" className="w-full">
              <TabsList className="w-full justify-start bg-zinc-900/60 border border-zinc-800 p-1">
                <TabsTrigger value="overview">Overview</TabsTrigger>
                <TabsTrigger value="disks">Storage ({vm.datastores?.length || 0})</TabsTrigger>
                <TabsTrigger value="nics">Network ({vm.network_interfaces?.length || 0})</TabsTrigger>
                <TabsTrigger value="tags">Tags & Metadata</TabsTrigger>
              </TabsList>

              {/* Overview Tab */}
              <TabsContent value="overview" className="mt-4 space-y-3 text-sm">
                <div className="rounded-xl border border-zinc-800 bg-zinc-900/30 p-4 space-y-2.5">
                  <div className="flex justify-between py-1 border-b border-zinc-800/60">
                    <span className="text-zinc-500 text-xs">Primary IP</span>
                    <span className="font-mono text-xs text-zinc-200">
                      {vm.guest_ip_address || "None"}
                    </span>
                  </div>
                  <div className="flex justify-between py-1 border-b border-zinc-800/60">
                    <span className="text-zinc-500 text-xs">Guest OS</span>
                    <span className="text-xs text-zinc-200 font-medium truncate max-w-[280px]">
                      {vm.guest_os || "Unknown"}
                    </span>
                  </div>
                  <div className="flex justify-between py-1 border-b border-zinc-800/60">
                    <span className="text-zinc-500 text-xs">Guest Hostname</span>
                    <span className="text-xs text-zinc-200">{vm.guest_host_name || "N/A"}</span>
                  </div>
                  <div className="flex justify-between py-1 border-b border-zinc-800/60">
                    <span className="text-zinc-500 text-xs">Folder Hierarchy</span>
                    <span className="text-xs text-zinc-300 font-mono flex items-center gap-1 truncate max-w-[280px]">
                      <FolderTree className="h-3 w-3 text-yellow-600" />
                      {vm.folder_path || "Root"}
                    </span>
                  </div>
                  <div className="flex justify-between py-1 border-b border-zinc-800/60">
                    <span className="text-zinc-500 text-xs">Instance UUID</span>
                    <span className="text-[11px] font-mono text-zinc-400 truncate max-w-[280px]">
                      {vm.instance_uuid}
                    </span>
                  </div>
                  {vm.bios_uuid && (
                    <div className="flex justify-between py-1 border-b border-zinc-800/60">
                      <span className="text-zinc-500 text-xs">BIOS UUID</span>
                      <span className="text-[11px] font-mono text-zinc-400 truncate max-w-[280px]">
                        {vm.bios_uuid}
                      </span>
                    </div>
                  )}
                  <div className="flex justify-between py-1">
                    <span className="text-zinc-500 text-xs">First Seen</span>
                    <span className="text-xs text-zinc-400 flex items-center gap-1">
                      <Clock className="h-3 w-3" />
                      {new Date(vm.created_at).toLocaleString()}
                    </span>
                  </div>
                </div>

                {vm.annotation && (
                  <div className="rounded-xl border border-zinc-800 bg-zinc-900/30 p-4">
                    <div className="text-xs font-semibold text-zinc-400 mb-1 flex items-center gap-1.5">
                      <FileText className="h-3.5 w-3.5 text-yellow-600" />
                      Notes / Annotation
                    </div>
                    <p className="text-xs text-zinc-300 whitespace-pre-wrap">{vm.annotation}</p>
                  </div>
                )}
              </TabsContent>

              {/* Disks Tab */}
              <TabsContent value="disks" className="mt-4 space-y-3">
                {vm.datastores && vm.datastores.length > 0 ? (
                  vm.datastores.map((ds) => {
                    const capGB = (ds.capacity_bytes / (1024 * 1024 * 1024)).toFixed(1);
                    const freeGB = (ds.free_bytes / (1024 * 1024 * 1024)).toFixed(1);
                    return (
                      <div
                        key={ds.id}
                        className="p-3.5 rounded-xl border border-zinc-800 bg-zinc-900/40 flex items-center justify-between"
                      >
                        <div className="flex items-center gap-3">
                          <HardDrive className="h-4 w-4 text-yellow-600" />
                          <div>
                            <div className="text-sm font-semibold text-zinc-100">{ds.name}</div>
                            <div className="text-[11px] text-zinc-500 font-mono">
                              {ds.type} • {ds.moref}
                            </div>
                          </div>
                        </div>
                        <div className="text-right">
                          <div className="text-xs font-bold text-zinc-200">{capGB} GB</div>
                          <div className="text-[10px] text-zinc-500">{freeGB} GB free</div>
                        </div>
                      </div>
                    );
                  })
                ) : (
                  <div className="p-6 text-center text-xs text-zinc-500 border border-zinc-800 rounded-xl">
                    No attached datastores reported.
                  </div>
                )}
              </TabsContent>

              {/* Network Interfaces Tab */}
              <TabsContent value="nics" className="mt-4 space-y-3">
                {vm.network_interfaces && vm.network_interfaces.length > 0 ? (
                  vm.network_interfaces.map((nic) => (
                    <div
                      key={nic.id}
                      className="p-3.5 rounded-xl border border-zinc-800 bg-zinc-900/40 flex items-center justify-between"
                    >
                      <div className="flex items-center gap-3">
                        <Network className="h-4 w-4 text-blue-500" />
                        <div>
                          <div className="text-xs font-mono font-bold text-zinc-100">
                            {nic.mac_address}
                          </div>
                          <div className="text-[11px] text-zinc-500">
                            Device Key: {nic.device_key}
                          </div>
                        </div>
                      </div>
                      <div className="text-right">
                        <div className="text-xs font-mono text-zinc-200">
                          {nic.ip_address || "No IP assigned"}
                        </div>
                      </div>
                    </div>
                  ))
                ) : (
                  <div className="p-6 text-center text-xs text-zinc-500 border border-zinc-800 rounded-xl">
                    No network interfaces reported.
                  </div>
                )}
              </TabsContent>

              {/* Tags & Metadata Tab */}
              <TabsContent value="tags" className="mt-4 space-y-4">
                <div>
                  <h4 className="text-xs font-semibold text-zinc-400 mb-2 flex items-center gap-1.5">
                    <Tag className="h-3.5 w-3.5 text-yellow-600" />
                    vSphere Tags ({vm.tags?.length || 0})
                  </h4>
                  {vm.tags && vm.tags.length > 0 ? (
                    <div className="flex flex-wrap gap-2">
                      {vm.tags.map((t, idx) => (
                        <span
                          key={idx}
                          className="px-2.5 py-1 rounded-md bg-zinc-900 border border-zinc-800 text-xs font-medium text-zinc-200"
                        >
                          <span className="text-zinc-500 font-normal">{t.category_name}:</span>{" "}
                          {t.tag_name}
                        </span>
                      ))}
                    </div>
                  ) : (
                    <p className="text-xs text-zinc-500">No tags assigned.</p>
                  )}
                </div>

                <div>
                  <h4 className="text-xs font-semibold text-zinc-400 mb-2 flex items-center gap-1.5">
                    <FileText className="h-3.5 w-3.5 text-yellow-600" />
                    Custom Attributes ({vm.custom_attributes?.length || 0})
                  </h4>
                  {vm.custom_attributes && vm.custom_attributes.length > 0 ? (
                    <div className="space-y-1.5">
                      {vm.custom_attributes.map((ca, idx) => (
                        <div
                          key={idx}
                          className="p-2.5 rounded-lg border border-zinc-800 bg-zinc-900/30 flex justify-between text-xs"
                        >
                          <span className="text-zinc-400 font-medium">{ca.name}</span>
                          <span className="text-zinc-200 font-mono">{ca.value}</span>
                        </div>
                      ))}
                    </div>
                  ) : (
                    <p className="text-xs text-zinc-500">No custom attributes defined.</p>
                  )}
                </div>
              </TabsContent>
            </Tabs>
          </div>
        ) : null}
      </div>
    </div>
    </>
  );
}
