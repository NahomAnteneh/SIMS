"use client";

import React, { useState, useEffect, useRef, useCallback, Suspense } from "react";
import { useRouter, usePathname, useSearchParams } from "next/navigation";
import {
  Search,
  RefreshCw,
  Loader2,
  Monitor,
  ArrowRight,
  X,
  Bell,
  CheckCheck,
  ExternalLink,
  ChevronRight,
  Layers,
  Terminal,
} from "lucide-react";
import { simsApi } from "@/lib/api/client";
import { VMListItem, AuditEventItem } from "@/lib/api/types";
import { VMDetailSheet } from "@/components/vm/vm-detail-sheet";
import { parseSearchQuery, SYNTAX_TOKENS } from "@/lib/search/parser";

const TopbarContent = () => {
  const router = useRouter();
  const pathname = usePathname();
  const searchParams = useSearchParams();
  const searchInputRef = useRef<HTMLInputElement>(null);

  // Search state
  const [searchQuery, setSearchQuery] = useState("");
  const [results, setResults] = useState<VMListItem[]>([]);
  const [isSearching, setIsSearching] = useState(false);
  const [isOpen, setIsOpen] = useState(false);
  const [selectedIndex, setSelectedIndex] = useState<number>(-1);
  const [selectedVmId, setSelectedVmId] = useState<string | null>(null);
  const [recentSearches, setRecentSearches] = useState<string[]>([]);

  // Notification state
  const [isNotificationsOpen, setIsNotificationsOpen] = useState(false);
  const [recentEvents, setRecentEvents] = useState<AuditEventItem[]>([]);
  const [unreadCount, setUnreadCount] = useState<number>(0);
  const [loadingEvents, setLoadingEvents] = useState(false);

  // Load recent searches on mount
  useEffect(() => {
    try {
      const stored = localStorage.getItem("sims_recent_searches");
      if (stored) {
        setRecentSearches(JSON.parse(stored));
      }
    } catch {
      // ignore
    }
  }, []);

  const saveRecentSearch = (query: string) => {
    const q = query.trim();
    if (!q) return;
    try {
      const updated = [q, ...recentSearches.filter((item) => item !== q)].slice(0, 5);
      setRecentSearches(updated);
      localStorage.setItem("sims_recent_searches", JSON.stringify(updated));
    } catch {
      // ignore
    }
  };

  const clearRecentSearches = () => {
    try {
      setRecentSearches([]);
      localStorage.removeItem("sims_recent_searches");
    } catch {
      // ignore
    }
  };

  const removeRecentSearch = (e: React.MouseEvent, q: string) => {
    e.stopPropagation();
    try {
      const updated = recentSearches.filter((item) => item !== q);
      setRecentSearches(updated);
      localStorage.setItem("sims_recent_searches", JSON.stringify(updated));
    } catch {
      // ignore
    }
  };

  // Sync unread notification count
  const syncUnreadCount = useCallback(() => {
    try {
      const stored = localStorage.getItem("sims_unread_alerts");
      if (stored !== null) {
        setUnreadCount(parseInt(stored, 10) || 0);
        return;
      }
    } catch {
      // ignore
    }

    simsApi
      .listAuditEvents({ limit: 10 })
      .then((events) => {
        const count = Math.min(events.length, 5);
        setUnreadCount(count);
        try {
          localStorage.setItem("sims_unread_alerts", count.toString());
        } catch {
          // ignore
        }
      })
      .catch(() => setUnreadCount(0));
  }, []);

  const loadRecentEvents = useCallback(async () => {
    setLoadingEvents(true);
    try {
      const evs = await simsApi.listAuditEvents({ limit: 8 });
      setRecentEvents(evs || []);
    } catch (err) {
      console.error("Failed to load audit events in topbar:", err);
    } finally {
      setLoadingEvents(false);
    }
  }, []);

  useEffect(() => {
    syncUnreadCount();
    const handleReadEvent = () => syncUnreadCount();
    window.addEventListener("sims-alerts-read", handleReadEvent);
    return () => window.removeEventListener("sims-alerts-read", handleReadEvent);
  }, [syncUnreadCount]);

  // Global keyboard shortcuts: "/" or "Cmd+K" to focus search, "Esc" to close
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      const isInput =
        document.activeElement?.tagName === "INPUT" ||
        document.activeElement?.tagName === "TEXTAREA";

      if ((e.metaKey || e.ctrlKey) && e.key === "k") {
        e.preventDefault();
        setIsOpen(true);
        searchInputRef.current?.focus();
      } else if (e.key === "/" && !isInput) {
        e.preventDefault();
        setIsOpen(true);
        searchInputRef.current?.focus();
      } else if (e.key === "Escape") {
        setIsOpen(false);
        setIsNotificationsOpen(false);
      }
    };

    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, []);

  // Debounced search with 180ms delay
  useEffect(() => {
    const trimmed = searchQuery.trim();
    if (!trimmed || trimmed.length < 2) {
      setResults([]);
      setIsSearching(false);
      setSelectedIndex(-1);
      return;
    }

    setIsSearching(true);
    const timeout = setTimeout(async () => {
      try {
        const parsed = parseSearchQuery(trimmed);
        let res;
        if (parsed.isStructured && parsed.filter) {
          res = await simsApi.structuredSearchVMs({
            filter: parsed.filter,
            limit: 10,
          });
        } else {
          res = await simsApi.quickSearchVMs(parsed.freeText || trimmed, 10);
        }
        setResults(res.items || []);
        setSelectedIndex(-1);
      } catch (err) {
        console.error("Search query execution failed:", err);
        setResults([]);
      } finally {
        setIsSearching(false);
      }
    }, 180);

    return () => clearTimeout(timeout);
  }, [searchQuery]);

  // Sync with global clear event
  useEffect(() => {
    const onClear = () => {
      setSearchQuery("");
      setResults([]);
    };
    window.addEventListener("sims-clear-search", onClear);
    return () => window.removeEventListener("sims-clear-search", onClear);
  }, []);

  const handleSelectVm = (vmId: string) => {
    if (searchQuery.trim()) {
      saveRecentSearch(searchQuery.trim());
    }
    setSelectedVmId(vmId);
    setIsOpen(false);
  };

  const handleExecuteSearch = (queryStr: string) => {
    const q = queryStr.trim();
    if (!q) return;
    saveRecentSearch(q);
    setIsOpen(false);
    setSearchQuery("");

    if (pathname === "/compute/vms") {
      const currentParams = new URLSearchParams(searchParams ? searchParams.toString() : "");
      const existing = currentParams.getAll("q");
      if (!existing.includes(q)) {
        currentParams.append("q", q);
        router.push(`/compute/vms?${currentParams.toString()}`);
      }
    } else {
      router.push(`/compute/vms?q=${encodeURIComponent(q)}`);
    }
  };

  const handleClearSearch = () => {
    setSearchQuery("");
    setResults([]);
    if (pathname === "/compute/vms") {
      router.push("/compute/vms");
    }
    window.dispatchEvent(new Event("sims-clear-search"));
  };

  const handleSearchKeyDown = (e: React.KeyboardEvent<HTMLInputElement>) => {
    if (e.key === "ArrowDown") {
      e.preventDefault();
      setSelectedIndex((prev) => Math.min(prev + 1, results.length - 1));
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      setSelectedIndex((prev) => Math.max(prev - 1, -1));
    } else if (e.key === "Enter") {
      e.preventDefault();
      if (selectedIndex >= 0 && results[selectedIndex]) {
        handleSelectVm(results[selectedIndex].id);
      } else if (searchQuery.trim()) {
        handleExecuteSearch(searchQuery);
      }
    }
  };

  const handleInsertToken = (prefix: string) => {
    setSearchQuery((prev) => {
      const trimmed = prev.trim();
      return trimmed ? `${trimmed} ${prefix}` : prefix;
    });
    setIsOpen(true);
    searchInputRef.current?.focus();
  };

  // Contextual autocomplete suggestions for active syntax tokens
  const activeTokenMatch = searchQuery.match(/(?:^|\s)([a-zA-Z0-9_-]+):(\S*)$/);
  const activeTokenKey = activeTokenMatch ? activeTokenMatch[1].toLowerCase() : null;

  const tokenSuggestions: Record<string, string[]> = {
    power: ["on", "off", "suspended"],
    ip: ["10.", "10.0.0.0/24", "192.168.1.0/24", "172.16.0.0/16"],
    os: ["linux", "windows", "photon", "rhel", "ubuntu"],
    vm: ["worker", "web", "db", "app", "gateway"],
  };

  const handleApplySuggestion = (val: string) => {
    if (!activeTokenMatch) return;
    const prefix = `${activeTokenMatch[1]}:`;
    const fullPrefixWithVal = `${prefix}${val}`;
    const lastIndex = searchQuery.lastIndexOf(prefix);
    if (lastIndex !== -1) {
      const updated = searchQuery.slice(0, lastIndex) + fullPrefixWithVal + " ";
      setSearchQuery(updated);
      searchInputRef.current?.focus();
    }
  };

  const handleMarkAllRead = () => {
    try {
      localStorage.setItem("sims_unread_alerts", "0");
      localStorage.setItem("sims_alerts_read_time", Date.now().toString());
      setUnreadCount(0);
      window.dispatchEvent(new Event("sims-alerts-read"));
    } catch {
      // ignore
    }
  };

  const toggleNotifications = () => {
    if (!isNotificationsOpen) {
      loadRecentEvents();
    }
    setIsNotificationsOpen(!isNotificationsOpen);
  };

  return (
    <>
      <header className="h-14 border-b border-zinc-900 bg-[#0a0a0a] flex items-center justify-between px-4 gap-4 shrink-0 relative z-30 w-full select-none">
        {/* Left Section: Platform Title */}
        <div className="flex items-center gap-3 shrink-0">
          <div className="flex items-center gap-2">
            <span className="text-xs font-mono font-bold text-yellow-500 uppercase tracking-widest">
              SIMS
            </span>
            <span className="text-zinc-700">/</span>
            <span className="text-xs font-mono text-zinc-400 uppercase tracking-wider">
              FLEET QUERY
            </span>
          </div>
        </div>

        {/* Center: Full-Span GitHub-Style Search Bar */}
        <div className="relative flex-1 min-w-0">
          <div className="relative flex items-center">
            <Search className="absolute left-3 h-4 w-4 text-zinc-500 pointer-events-none" />
            <input
              ref={searchInputRef}
              type="text"
              placeholder={
                pathname === "/compute/vms"
                  ? (searchParams?.has("q") || searchParams?.has("host_id") || searchParams?.has("cluster_id") || searchParams?.has("datastore_id") || searchParams?.has("network_id") || searchParams?.has("datacenter_id"))
                    ? "Type to add another filter... (e.g. power:on, 10.0.0.0/24, spark) [Press Enter]"
                    : "Global search / filter VMs... (e.g. spark, power:on, 10.0.0.0/24) [Press Enter]"
                  : "Search fleet by name, IP, power state, host... (Press '/' to search)"
              }
              value={searchQuery}
              onFocus={() => setIsOpen(true)}
              onKeyDown={handleSearchKeyDown}
              onChange={(e) => {
                setSearchQuery(e.target.value);
                setIsOpen(true);
              }}
              className="w-full h-9 pl-9 pr-16 bg-[#0f0f0f] border border-zinc-800 text-sm text-zinc-100 placeholder:text-zinc-500 focus:outline-none focus:border-yellow-500/80 focus:bg-[#121212] transition-colors font-mono text-xs rounded-none"
            />
            <div className="absolute right-2.5 flex items-center gap-1">
              {searchQuery || (pathname === "/compute/vms" && (searchParams?.has("q") || searchParams?.has("host_id") || searchParams?.has("cluster_id") || searchParams?.has("datastore_id") || searchParams?.has("network_id") || searchParams?.has("datacenter_id"))) ? (
                <button
                  type="button"
                  onClick={handleClearSearch}
                  className="p-1 text-zinc-500 hover:text-yellow-400 transition-colors"
                  title="Clear search and filters"
                >
                  <X className="h-3.5 w-3.5" />
                </button>
              ) : (
                <kbd className="px-1.5 py-0.5 text-[10px] font-mono text-zinc-500 bg-zinc-900 border border-zinc-800 rounded-none">
                  /
                </kbd>
              )}
            </div>
          </div>

          {/* Command Palette Dropdown */}
          {isOpen && (
            <>
              <div
                className="fixed inset-0 z-40 bg-black/40"
                onClick={() => setIsOpen(false)}
              />
              <div className="absolute left-0 top-full mt-1 w-full border border-zinc-700 bg-[#0c0c0c] shadow-2xl z-50 overflow-hidden font-sans rounded-none">
                {/* Syntax Filter Tokens */}
                <div className="p-2.5 border-b border-zinc-800 bg-[#0e0e0e]">
                  <div className="flex items-center justify-between mb-1.5">
                    <span className="text-[10px] font-mono uppercase tracking-wider text-zinc-400 font-semibold">
                      Filter Syntax Tokens (Click to append)
                    </span>
                    <span className="text-[10px] text-zinc-500 font-mono">
                      combine multiple filters freely
                    </span>
                  </div>
                  <div className="flex flex-wrap gap-1">
                    {SYNTAX_TOKENS.map((tok) => (
                      <button
                        key={tok.prefix}
                        type="button"
                        onClick={() => handleInsertToken(tok.prefix)}
                        title={`${tok.description} (${tok.example})`}
                        className="px-2 py-0.5 text-[11px] font-mono border border-zinc-800 bg-zinc-900 text-yellow-400 hover:bg-zinc-800 hover:border-yellow-600/60 transition-colors rounded-none"
                      >
                        {tok.prefix}
                      </button>
                    ))}
                  </div>
                </div>

                {/* Contextual Autocomplete Suggestions for active token */}
                {activeTokenKey && tokenSuggestions[activeTokenKey] && (
                  <div className="px-3 py-1.5 bg-yellow-500/10 border-b border-yellow-500/20 flex items-center gap-2">
                    <span className="text-[10px] font-mono uppercase tracking-wider text-yellow-400 font-semibold flex items-center gap-1 shrink-0">
                      <Terminal className="h-3 w-3" />
                      Suggestions for {activeTokenKey}:
                    </span>
                    <div className="flex items-center gap-1.5 flex-wrap">
                      {tokenSuggestions[activeTokenKey].map((sug) => (
                        <button
                          key={sug}
                          type="button"
                          onClick={() => handleApplySuggestion(sug)}
                          className="px-1.5 py-0.5 text-xs font-mono bg-zinc-900 border border-yellow-600/50 text-yellow-200 hover:bg-yellow-500/20 rounded-none transition-colors"
                        >
                          {sug}
                        </button>
                      ))}
                    </div>
                  </div>
                )}

                {/* Quick Filters & Recent Searches when query is empty */}
                {!searchQuery.trim() && (
                  <div className="p-3 border-b border-zinc-900 bg-[#0a0a0a] space-y-3">
                    {/* Quick Filters */}
                    <div>
                      <div className="text-[10px] font-mono uppercase tracking-wider text-zinc-500 mb-1.5">
                        Quick Preset Filters
                      </div>
                      <div className="flex flex-wrap gap-1.5">
                        {[
                          { label: "Powered On", query: "power:on" },
                          { label: "Powered Off", query: "power:off" },
                          { label: "Suspended", query: "power:suspended" },
                          { label: "Linux Guests", query: "os:linux" },
                          { label: "Windows Guests", query: "os:windows" },
                        ].map((qf) => (
                          <button
                            key={qf.query}
                            type="button"
                            onClick={() => handleExecuteSearch(qf.query)}
                            className="px-2 py-1 text-xs font-mono bg-zinc-900/80 border border-zinc-800 text-zinc-300 hover:text-white hover:border-zinc-700 transition-colors rounded-none"
                          >
                            <span className="text-yellow-500 mr-1.5 font-bold">›</span>
                            {qf.label} ({qf.query})
                          </button>
                        ))}
                      </div>
                    </div>

                    {/* Recent Searches */}
                    {recentSearches.length > 0 && (
                      <div>
                        <div className="flex items-center justify-between text-[10px] font-mono uppercase tracking-wider text-zinc-500 mb-1.5">
                          <span>Recent Searches</span>
                          <button
                            type="button"
                            onClick={clearRecentSearches}
                            className="hover:text-zinc-300 transition-colors"
                          >
                            Clear All
                          </button>
                        </div>
                        <div className="flex flex-wrap gap-1.5">
                          {recentSearches.map((sq) => (
                            <div
                              key={sq}
                              onClick={() => handleExecuteSearch(sq)}
                              className="group flex items-center gap-1.5 px-2 py-0.5 text-xs font-mono bg-zinc-900 border border-zinc-800 text-zinc-300 hover:text-yellow-400 hover:border-yellow-500/50 transition-colors rounded-none cursor-pointer"
                            >
                              <span>{sq}</span>
                              <button
                                type="button"
                                onClick={(e) => removeRecentSearch(e, sq)}
                                className="opacity-40 group-hover:opacity-100 hover:text-red-400 transition-opacity p-0.5"
                                title="Remove search"
                              >
                                <X className="h-2.5 w-2.5" />
                              </button>
                            </div>
                          ))}
                        </div>
                      </div>
                    )}
                  </div>
                )}

                {/* Live Search Results */}
                {searchQuery.trim().length >= 2 ? (
                  <div className="max-h-80 overflow-y-auto divide-y divide-zinc-900">
                    <div className="px-3 py-1.5 bg-[#0a0a0a] text-[10px] font-mono uppercase tracking-wider text-zinc-500 flex items-center justify-between">
                      <span>Matching Instances ({results.length})</span>
                      {isSearching && (
                        <Loader2 className="h-3 w-3 animate-spin text-yellow-500" />
                      )}
                    </div>

                    {results.length > 0 ? (
                      results.map((vm, index) => {
                        const isSelected = index === selectedIndex;
                        const isOn = vm.power_state === "poweredOn";
                        const isSuspended = vm.power_state === "suspended";

                        return (
                          <div
                            key={vm.id}
                            onClick={() => handleSelectVm(vm.id)}
                            onMouseEnter={() => setSelectedIndex(index)}
                            className={`px-3 py-2.5 cursor-pointer flex items-center justify-between transition-colors border-b border-zinc-900 ${
                              isSelected
                                ? "bg-zinc-800/90 text-white border-l-2 border-l-yellow-500 pl-[10px]"
                                : "hover:bg-zinc-900/60 text-zinc-300"
                            }`}
                          >
                            <div className="flex items-center gap-3 min-w-0 flex-1">
                              <span
                                className={`h-2.5 w-2.5 shrink-0 rounded-none ${
                                  isOn
                                    ? "bg-emerald-400 ring-2 ring-emerald-500/20"
                                    : isSuspended
                                    ? "bg-amber-400 ring-2 ring-amber-500/20"
                                    : "bg-zinc-600"
                                }`}
                                title={vm.power_state}
                              />
                              <div className="min-w-0 flex-1">
                                <div className="text-xs font-semibold text-zinc-100 truncate flex items-center gap-2">
                                  <span className="font-mono">{vm.name}</span>
                                  {vm.guest_ip_address && (
                                    <span className="text-[11px] font-mono px-1.5 py-0.2 bg-emerald-950/40 border border-emerald-800/60 text-emerald-300 rounded-none">
                                      {vm.guest_ip_address}
                                    </span>
                                  )}
                                </div>
                                <div className="text-[10px] font-mono text-zinc-500 flex items-center gap-2 mt-0.5">
                                  <span className="text-zinc-400">{vm.moref}</span>
                                  <span>•</span>
                                  <span>{vm.cpu_cores || 1} vCPU</span>
                                  <span>•</span>
                                  <span>{Math.round((vm.memory_mb || 1024) / 1024)} GB RAM</span>
                                  {vm.guest_host_name && (
                                    <>
                                      <span>•</span>
                                      <span className="text-zinc-400 truncate max-w-[140px]">
                                        {vm.guest_host_name}
                                      </span>
                                    </>
                                  )}
                                </div>
                              </div>
                            </div>

                            <div className="flex items-center gap-2 shrink-0 ml-2">
                              <span
                                className={`text-[10px] font-mono uppercase px-1.5 py-0.5 border rounded-none ${
                                  isOn
                                    ? "border-emerald-800/60 text-emerald-400 bg-emerald-950/20"
                                    : isSuspended
                                    ? "border-amber-800/60 text-amber-400 bg-amber-950/20"
                                    : "border-zinc-800 text-zinc-500 bg-zinc-900/40"
                                }`}
                              >
                                {vm.power_state}
                              </span>
                              <button
                                type="button"
                                onClick={(e) => {
                                  e.stopPropagation();
                                  handleExecuteSearch(vm.name);
                                }}
                                title="Filter in Table"
                                className="p-1 text-zinc-500 hover:text-yellow-400 hover:bg-zinc-800 rounded-none transition-colors"
                              >
                                <Layers className="h-3 w-3" />
                              </button>
                              <ArrowRight className={`h-3 w-3 ${isSelected ? "text-yellow-400" : "text-zinc-600"}`} />
                            </div>
                          </div>
                        );
                      })
                    ) : isSearching ? (
                      <div className="py-8 text-center text-xs text-zinc-500 flex items-center justify-center gap-2">
                        <Loader2 className="h-4 w-4 animate-spin text-yellow-500" />
                        <span>Querying inventory fleet...</span>
                      </div>
                    ) : (
                      <div className="py-8 text-center text-xs text-zinc-500 font-mono space-y-1">
                        <div>No virtual machines match &quot;{searchQuery}&quot;.</div>
                        <div className="text-[11px] text-zinc-600">
                          Try matching an IP prefix (e.g. <code className="text-zinc-400">10.</code>), power state (e.g. <code className="text-zinc-400">power:on</code>), or press Enter to search full table.
                        </div>
                      </div>
                    )}

                    {/* View all in inventory footer item */}
                    <div
                      onClick={() => handleExecuteSearch(searchQuery)}
                      className="p-2.5 bg-[#0a0a0a] hover:bg-zinc-900 text-xs font-mono text-yellow-400 cursor-pointer flex items-center justify-between border-t border-zinc-800"
                    >
                      <div className="flex items-center gap-2">
                        <Search className="h-3.5 w-3.5" />
                        <span>Open full inventory table for &quot;{searchQuery.trim()}&quot;</span>
                      </div>
                      <kbd className="px-1.5 py-0.5 text-[10px] bg-zinc-800 border border-zinc-700 text-zinc-300 rounded-none">
                        Enter
                      </kbd>
                    </div>
                  </div>
                ) : (
                  <div className="p-3 text-[11px] text-zinc-500 font-mono">
                    Type to search instances or enter syntax like{" "}
                    <code className="text-yellow-400">vm:worker power:on ip:10.0.0.0/24</code>.
                  </div>
                )}

                {/* Footer Command Hints */}
                <div className="p-2 border-t border-zinc-800 bg-[#0a0a0a] flex items-center justify-between text-[10px] font-mono text-zinc-500">
                  <div className="flex items-center gap-3">
                    <span>
                      <kbd className="px-1 py-0.2 bg-zinc-900 border border-zinc-800 text-zinc-400 rounded-none">
                        Enter
                      </kbd>{" "}
                      Search Fleet Table
                    </span>
                    <span>
                      <kbd className="px-1 py-0.2 bg-zinc-900 border border-zinc-800 text-zinc-400 rounded-none">
                        ↑↓
                      </kbd>{" "}
                      Select VM
                    </span>
                  </div>
                  <span>
                    <kbd className="px-1 py-0.2 bg-zinc-900 border border-zinc-800 text-zinc-400 rounded-none">
                      Esc
                    </kbd>{" "}
                    Close
                  </span>
                </div>
              </div>
            </>
          )}
        </div>

        {/* Right Section: Mutation Notifications & Quick Controls */}
        <div className="flex items-center gap-3 shrink-0">
          {/* Notifications Flyout Trigger */}
          <div className="relative">
            <button
              type="button"
              onClick={toggleNotifications}
              className={`p-2 border transition-colors relative flex items-center justify-center ${
                isNotificationsOpen
                  ? "border-yellow-500 bg-zinc-900 text-yellow-400"
                  : "border-zinc-800 bg-[#0f0f0f] text-zinc-400 hover:text-zinc-100 hover:border-zinc-700"
              }`}
              title="Inventory Mutations & Activity"
            >
              <Bell className="h-4 w-4" />
              {unreadCount > 0 && (
                <span className="absolute -top-1 -right-1 bg-yellow-500 text-black text-[9px] font-mono font-bold px-1 min-w-[16px] h-4 flex items-center justify-center">
                  {unreadCount}
                </span>
              )}
            </button>

            {/* Notifications Dropdown Drawer */}
            {isNotificationsOpen && (
              <>
                <div
                  className="fixed inset-0 z-40"
                  onClick={() => setIsNotificationsOpen(false)}
                />
                <div className="absolute right-0 top-full mt-2 w-96 border border-zinc-800 bg-[#0d0d0d] shadow-2xl z-50 overflow-hidden font-sans rounded-none">
                  <div className="p-3 border-b border-zinc-800 bg-[#0a0a0a] flex items-center justify-between">
                    <div className="flex items-center gap-2">
                      <Bell className="h-3.5 w-3.5 text-yellow-500" />
                      <span className="text-xs font-mono font-bold uppercase tracking-wider text-zinc-200">
                        Inventory Activity
                      </span>
                    </div>
                    {unreadCount > 0 && (
                      <button
                        type="button"
                        onClick={handleMarkAllRead}
                        className="text-[10px] font-mono text-zinc-400 hover:text-yellow-400 flex items-center gap-1"
                      >
                        <CheckCheck className="h-3 w-3" />
                        Mark Read
                      </button>
                    )}
                  </div>

                  <div className="max-h-80 overflow-y-auto divide-y divide-zinc-900">
                    {loadingEvents ? (
                      <div className="py-8 text-center text-xs text-zinc-500 flex items-center justify-center gap-2">
                        <Loader2 className="h-4 w-4 animate-spin text-yellow-500" />
                        <span>Loading audit ledger...</span>
                      </div>
                    ) : recentEvents.length === 0 ? (
                      <div className="p-6 text-center text-xs text-zinc-500 font-mono">
                        No recent mutation activity.
                      </div>
                    ) : (
                      recentEvents.map((ev) => {
                        const isSuccess = ev.outcome?.toUpperCase() === "SUCCESS";
                        return (
                          <div
                            key={ev.id}
                            className="p-3 hover:bg-zinc-900/40 transition-colors"
                          >
                            <div className="flex items-center justify-between">
                              <span className="font-mono text-xs font-semibold text-zinc-200">
                                {ev.action}
                              </span>
                              <span
                                className={`text-[9px] font-mono uppercase px-1 py-0.2 border ${
                                  isSuccess
                                    ? "text-emerald-400 border-emerald-500/30 bg-emerald-500/10"
                                    : "text-rose-400 border-rose-500/30 bg-rose-500/10"
                                }`}
                              >
                                {ev.outcome}
                              </span>
                            </div>
                            <div className="text-[11px] text-zinc-400 mt-1 truncate">
                              Target: {ev.target_id || "system"}
                            </div>
                            <div className="text-[10px] font-mono text-zinc-500 mt-1 flex items-center justify-between">
                              <span>Actor: {ev.actor}</span>
                              <span>{new Date(ev.created_at).toLocaleTimeString()}</span>
                            </div>
                          </div>
                        );
                      })
                    )}
                  </div>

                  <div className="p-2.5 border-t border-zinc-800 bg-[#0a0a0a]">
                    <button
                      type="button"
                      onClick={() => {
                        setIsNotificationsOpen(false);
                        router.push("/alerts");
                      }}
                      className="w-full text-center py-1.5 text-xs font-mono text-yellow-400 hover:text-yellow-300 hover:bg-zinc-900 transition-colors flex items-center justify-center gap-1.5"
                    >
                      <span>Open Full Audit Log</span>
                      <ChevronRight className="h-3 w-3" />
                    </button>
                  </div>
                </div>
              </>
            )}
          </div>

          {/* Quick Refresh Window */}
          <button
            type="button"
            onClick={() => window.location.reload()}
            title="Refresh Fleet"
            className="p-2 border border-zinc-800 bg-[#0f0f0f] text-zinc-400 hover:text-zinc-100 hover:border-zinc-700 transition-colors"
          >
            <RefreshCw className="h-4 w-4" />
          </button>
        </div>
      </header>

      {/* Global VM Detail Sheet */}
      <VMDetailSheet vmId={selectedVmId} onClose={() => setSelectedVmId(null)} />
    </>
  );
};

export const Topbar = () => {
  return (
    <Suspense
      fallback={
        <header className="h-14 border-b border-zinc-900 bg-[#0a0a0a] flex items-center justify-between px-4 gap-4 shrink-0 select-none" />
      }
    >
      <TopbarContent />
    </Suspense>
  );
};