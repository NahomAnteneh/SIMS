"use client";

import { Fragment, useState, useEffect, useRef } from "react";
import { usePathname } from "next/navigation";
import { Sidebar, Search, Plus, Menu, RefreshCw, Loader2, Monitor, Power, ArrowRight, X } from "lucide-react";
import { simsApi } from "@/lib/api/client";
import { VMListItem } from "@/lib/api/types";
import { VMDetailSheet } from "@/components/vm/vm-detail-sheet";

export const Topbar = () => {
  const pathname = usePathname();
  const searchInputRef = useRef<HTMLInputElement>(null);

  const [searchQuery, setSearchQuery] = useState("");
  const [results, setResults] = useState<VMListItem[]>([]);
  const [isSearching, setIsSearching] = useState(false);
  const [isOpen, setIsOpen] = useState(false);
  const [selectedVmId, setSelectedVmId] = useState<string | null>(null);

  // Safely split the URL path into an array, removing empty strings
  const segments = pathname ? pathname.split("/").filter(Boolean) : [];

  // Helper to format paths (e.g., "data-sources" -> "Data Sources")
  const formatLabel = (text: string) => {
    return text
      .split("-")
      .map((word) => word.charAt(0).toUpperCase() + word.slice(1))
      .join(" ");
  };

  // Keyboard shortcut listener for ⌘K / Ctrl+K and Escape
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key === "k") {
        e.preventDefault();
        setIsOpen(true);
        searchInputRef.current?.focus();
      } else if (e.key === "Escape") {
        setIsOpen(false);
      }
    };

    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, []);

  // Debounced search
  useEffect(() => {
    if (!searchQuery.trim() || searchQuery.trim().length < 2) {
      setResults([]);
      setIsSearching(false);
      return;
    }

    setIsSearching(true);
    const timeout = setTimeout(async () => {
      try {
        const res = await simsApi.quickSearchVMs(searchQuery.trim(), 10);
        setResults(res.items || []);
      } catch (err) {
        console.error("Quick search failed:", err);
        setResults([]);
      } finally {
        setIsSearching(false);
      }
    }, 300);

    return () => clearTimeout(timeout);
  }, [searchQuery]);

  const handleSelectVm = (vmId: string) => {
    setSelectedVmId(vmId);
    setIsOpen(false);
    setSearchQuery("");
  };

  return (
    <>
      <header className="h-14 border-b border-zinc-900 bg-[#0a0a0a] flex items-center justify-between px-6 shrink-0 relative z-30">
        <div className="flex items-center gap-4">
          <button className="text-zinc-500 hover:text-zinc-300 transition-colors">
            <Sidebar className="h-5 w-5" />
          </button>

          {/* Dynamic Breadcrumbs */}
          <div className="flex items-center text-sm text-zinc-500">
            {segments.length === 0 ? (
              <span className="text-zinc-200 font-medium">Dashboard</span>
            ) : (
              segments.map((segment, index) => {
                const isLast = index === segments.length - 1;
                return (
                  <Fragment key={`${segment}-${index}`}>
                    <span className={isLast ? "text-zinc-200 font-medium" : "text-zinc-500"}>
                      {formatLabel(segment)}
                    </span>
                    {!isLast && <span className="mx-2 text-zinc-700">›</span>}
                  </Fragment>
                );
              })
            )}
          </div>
        </div>

        <div className="flex items-center gap-4">
          {/* Quick Search */}
          <div className="relative">
            <div className="relative group">
              <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-zinc-500" />
              <input
                ref={searchInputRef}
                type="text"
                placeholder="Search VMs, IPs, MoRefs..."
                value={searchQuery}
                onFocus={() => setIsOpen(true)}
                onChange={(e) => {
                  setSearchQuery(e.target.value);
                  setIsOpen(true);
                }}
                className="w-72 bg-zinc-900/50 border border-zinc-800 rounded-md py-1.5 pl-9 pr-12 text-sm text-zinc-200 placeholder:text-zinc-600 focus:outline-none focus:border-zinc-700 transition-colors"
              />
              <div className="absolute right-2 top-1/2 -translate-y-1/2 flex items-center gap-1">
                {searchQuery ? (
                  <button
                    onClick={() => {
                      setSearchQuery("");
                      setResults([]);
                    }}
                    className="text-zinc-500 hover:text-zinc-300 p-0.5"
                  >
                    <X className="h-3.5 w-3.5" />
                  </button>
                ) : (
                  <>
                    <kbd className="text-[10px] font-mono text-zinc-500 bg-zinc-800 px-1 rounded">⌘</kbd>
                    <kbd className="text-[10px] font-mono text-zinc-500 bg-zinc-800 px-1 rounded">K</kbd>
                  </>
                )}
              </div>
            </div>

            {/* Dropdown Results */}
            {isOpen && searchQuery.length >= 2 && (
              <>
                <div
                  className="fixed inset-0 z-40"
                  onClick={() => setIsOpen(false)}
                />
                <div className="absolute right-0 top-full mt-2 w-96 rounded-xl border border-zinc-800 bg-[#121212] shadow-2xl p-2 z-50 overflow-hidden">
                  <div className="px-3 py-2 text-[11px] font-semibold text-zinc-500 uppercase tracking-wider border-b border-zinc-800 flex items-center justify-between">
                    <span>Virtual Machines</span>
                    {isSearching && <Loader2 className="h-3 w-3 animate-spin text-yellow-500" />}
                  </div>

                  <div className="max-h-72 overflow-y-auto py-1">
                    {results.length > 0 ? (
                      results.map((vm) => {
                        const isOn = vm.power_state === "poweredOn";
                        return (
                          <button
                            key={vm.id}
                            onClick={() => handleSelectVm(vm.id)}
                            className="w-full text-left px-3 py-2.5 rounded-lg hover:bg-zinc-800/60 transition-colors flex items-center justify-between group"
                          >
                            <div className="flex items-center gap-2.5 min-w-0">
                              <Monitor className="h-4 w-4 text-zinc-400 group-hover:text-yellow-500 transition-colors shrink-0" />
                              <div className="min-w-0">
                                <div className="text-sm font-medium text-zinc-200 truncate group-hover:text-zinc-100">
                                  {vm.name}
                                </div>
                                <div className="text-xs text-zinc-500 flex items-center gap-2">
                                  {vm.guest_ip_address && (
                                    <span className="font-mono text-zinc-400">{vm.guest_ip_address}</span>
                                  )}
                                  <span className="font-mono">{vm.id.slice(0, 8)}...</span>
                                </div>
                              </div>
                            </div>
                            <div className="flex items-center gap-2 shrink-0">
                              <span
                                className={`text-[10px] font-medium px-2 py-0.5 rounded-full flex items-center gap-1 ${
                                  isOn
                                    ? "text-emerald-400 bg-emerald-400/10"
                                    : "text-zinc-500 bg-zinc-800"
                                }`}
                              >
                                <Power className="h-2 w-2" />
                                {vm.power_state}
                              </span>
                              <ArrowRight className="h-3.5 w-3.5 text-zinc-600 group-hover:text-zinc-300 transition-colors" />
                            </div>
                          </button>
                        );
                      })
                    ) : isSearching ? (
                      <div className="py-8 text-center text-xs text-zinc-500 flex items-center justify-center gap-2">
                        <Loader2 className="h-4 w-4 animate-spin text-zinc-500" />
                        Searching inventory...
                      </div>
                    ) : (
                      <div className="py-8 text-center text-xs text-zinc-500">
                        No virtual machines matching "{searchQuery}"
                      </div>
                    )}
                  </div>
                </div>
              </>
            )}
          </div>

          {/* Actions */}
          <div className="flex items-center gap-3 text-zinc-400">
            <button
              onClick={() => window.location.reload()}
              title="Refresh"
              className="hover:text-zinc-200 transition-colors p-1"
            >
              <RefreshCw className="h-4 w-4" />
            </button>
          </div>
        </div>
      </header>

      {/* Global VM Detail Sheet */}
      <VMDetailSheet vmId={selectedVmId} onClose={() => setSelectedVmId(null)} />
    </>
  );
};