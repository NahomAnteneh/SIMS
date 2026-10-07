"use client";

import React, { useState, useEffect, useCallback } from "react";
import {
  Bell,
  RefreshCw,
  Search,
  Loader2,
  Clock,
  User,
  CheckCheck,
  CheckCircle2,
  AlertTriangle,
  XCircle,
  Info,
  ChevronDown,
  ChevronRight,
  ExternalLink,
  ShieldCheck,
  Terminal,
} from "lucide-react";
import { simsApi } from "@/lib/api/client";
import { AuditEventItem } from "@/lib/api/types";
import { Button } from "@/components/ui/button";
import { VMDetailSheet } from "@/components/vm/vm-detail-sheet";

type Severity = "CRITICAL" | "WARNING" | "SUCCESS" | "INFO";

interface SynthesizedInfo {
  title: string;
  description: string;
  severity: Severity;
  vmId?: string;
}

function synthesizeEvent(ev: AuditEventItem): SynthesizedInfo {
  const isFailure = ev.outcome?.toUpperCase() === "FAILURE";
  const action = ev.action?.toLowerCase() || "";
  const details = ev.details || {};

  // 1. VM Discovery / Creation
  if (action === "vm.discovered" || action === "vm.created") {
    const vmName = details.name || details.vm_name || ev.target_id;
    return {
      title: "New Virtual Machine Registered",
      description: `Instance "${vmName}" registered into inventory.`,
      severity: isFailure ? "CRITICAL" : "SUCCESS",
      vmId: ev.target_id,
    };
  }

  // 2. VM Power State Transition
  if (action === "vm.power_state" || action.startsWith("vm.power")) {
    const vmName = details.name || details.vm_name || ev.target_id;
    const newState = details.power_state || details.new_state || "updated";
    const isSuspended = String(newState).toLowerCase().includes("suspend");
    return {
      title: "VM Power State Transition",
      description: `Virtual machine "${vmName}" transitioned to ${newState}.`,
      severity: isFailure ? "CRITICAL" : isSuspended ? "WARNING" : "SUCCESS",
      vmId: ev.target_id,
    };
  }

  // 3. vCenter Resync
  if (action === "vcenter.resync") {
    return {
      title: "vCenter Inventory Resync",
      description: `vCenter connection resynchronized with inventory (${ev.target_id}).`,
      severity: isFailure ? "CRITICAL" : "INFO",
    };
  }

  // 4. vCenter Test Connection
  if (action === "vcenter.test_connection") {
    return {
      title: "vCenter Connectivity Test",
      description: `vCenter connection test ${ev.outcome?.toLowerCase() || "completed"}.`,
      severity: isFailure ? "CRITICAL" : "SUCCESS",
    };
  }

  // 5. User / RBAC Identity
  if (action.startsWith("user.") || action.startsWith("auth.")) {
    const targetUser = details.username || ev.target_id || ev.actor;
    return {
      title: `RBAC Identity: ${ev.action}`,
      description: `Identity update for ${targetUser} by ${ev.actor}.`,
      severity: isFailure ? "CRITICAL" : "INFO",
    };
  }

  // 6. Generic VM lifecycle
  if (action.startsWith("vm.")) {
    const vmName = details.name || details.vm_name || ev.target_id;
    return {
      title: `VM Lifecycle: ${ev.action}`,
      description: `Virtual machine operation on "${vmName}".`,
      severity: isFailure ? "CRITICAL" : "INFO",
      vmId: ev.target_id,
    };
  }

  // 7. Generic Fallback
  return {
    title: ev.action,
    description: `Invoked by ${ev.actor} against ${ev.target_id || "system"}.`,
    severity: isFailure ? "CRITICAL" : ev.outcome?.toUpperCase() === "WARNING" ? "WARNING" : "INFO",
    vmId: ev.target_id && ev.target_id.includes("-") && ev.target_id.length === 36 ? ev.target_id : undefined,
  };
}

export default function AlertsPage() {
  const [events, setEvents] = useState<AuditEventItem[]>([]);
  const [loading, setLoading] = useState(true);
  const [severityFilter, setSeverityFilter] = useState<string>("ALL");
  const [searchQuery, setSearchQuery] = useState("");
  const [expandedId, setExpandedId] = useState<string | null>(null);
  const [selectedVmId, setSelectedVmId] = useState<string | null>(null);
  const [readCheckpoint, setReadCheckpoint] = useState<number>(0);

  // Initialize read checkpoint from localStorage
  useEffect(() => {
    try {
      const stored = localStorage.getItem("sims_alerts_read_time");
      if (stored) {
        setReadCheckpoint(parseInt(stored, 10) || 0);
      }
    } catch {
      // ignore
    }
  }, []);

  const loadEvents = useCallback(async () => {
    setLoading(true);
    try {
      const data = await simsApi.listAuditEvents({ limit: 50 });
      setEvents(data || []);
    } catch (err) {
      console.error("Failed to load audit events", err);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    loadEvents();
  }, [loadEvents]);

  const handleMarkAllRead = () => {
    const now = Date.now();
    setReadCheckpoint(now);
    try {
      localStorage.setItem("sims_unread_alerts", "0");
      localStorage.setItem("sims_alerts_read_time", now.toString());
      window.dispatchEvent(new Event("sims-alerts-read"));
    } catch {
      // ignore
    }
  };

  const synthesizedList = events.map((ev) => ({
    event: ev,
    synthesized: synthesizeEvent(ev),
    isUnread: new Date(ev.created_at).getTime() > readCheckpoint,
  }));

  const filteredItems = synthesizedList.filter(({ event: ev, synthesized }) => {
    if (severityFilter !== "ALL" && synthesized.severity !== severityFilter) {
      return false;
    }

    if (!searchQuery) return true;
    const q = searchQuery.toLowerCase();
    return (
      synthesized.title.toLowerCase().includes(q) ||
      synthesized.description.toLowerCase().includes(q) ||
      ev.action?.toLowerCase().includes(q) ||
      ev.actor?.toLowerCase().includes(q) ||
      ev.target_id?.toLowerCase().includes(q)
    );
  });

  const criticalCount = synthesizedList.filter((i) => i.synthesized.severity === "CRITICAL").length;
  const warningCount = synthesizedList.filter((i) => i.synthesized.severity === "WARNING").length;
  const unreadCount = synthesizedList.filter((i) => i.isUnread).length;

  const getSeverityBadge = (severity: Severity) => {
    switch (severity) {
      case "CRITICAL":
        return (
          <span className="inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-[10px] font-bold uppercase tracking-wider bg-rose-500/15 text-rose-400 border border-rose-500/30">
            <XCircle className="h-3 w-3" />
            CRITICAL
          </span>
        );
      case "WARNING":
        return (
          <span className="inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-[10px] font-bold uppercase tracking-wider bg-amber-500/15 text-amber-400 border border-amber-500/30">
            <AlertTriangle className="h-3 w-3" />
            WARNING
          </span>
        );
      case "SUCCESS":
        return (
          <span className="inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-[10px] font-bold uppercase tracking-wider bg-emerald-500/15 text-emerald-400 border border-emerald-500/30">
            <CheckCircle2 className="h-3 w-3" />
            SUCCESS
          </span>
        );
      case "INFO":
      default:
        return (
          <span className="inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-[10px] font-bold uppercase tracking-wider bg-blue-500/15 text-blue-400 border border-blue-500/30">
            <Info className="h-3 w-3" />
            INFO
          </span>
        );
    }
  };

  return (
    <div className="space-y-6 max-w-7xl mx-auto p-2 md:p-4">
      {/* Page Header */}
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4">
        <div>
          <h1 className="text-2xl font-semibold text-zinc-100 flex items-center gap-2.5">
            <Bell className="h-6 w-6 text-yellow-500" />
            Notifications & Activity Feed
            {unreadCount > 0 && (
              <span className="text-xs px-2.5 py-0.5 rounded-full bg-yellow-500/20 text-yellow-400 font-mono font-medium">
                {unreadCount} unread
              </span>
            )}
          </h1>
          <p className="text-sm text-zinc-500 mt-1">
            Real-time feed of inventory mutations, cluster alerts, vCenter syncs, and security events.
          </p>
        </div>

        <div className="flex items-center gap-2.5 self-start sm:self-auto">
          <Button
            variant="outline"
            size="sm"
            onClick={handleMarkAllRead}
            disabled={unreadCount === 0}
            className="border-zinc-800 bg-zinc-900/60 text-zinc-300 hover:text-white hover:bg-zinc-800 gap-1.5 h-8 text-xs"
          >
            <CheckCheck className="h-3.5 w-3.5 text-yellow-500" />
            Mark All Read
          </Button>

          <Button
            variant="outline"
            size="sm"
            onClick={loadEvents}
            disabled={loading}
            className="border-zinc-800 bg-zinc-900/50 text-zinc-300 hover:text-white hover:bg-zinc-800 gap-1.5 h-8"
          >
            <RefreshCw className={`h-3.5 w-3.5 ${loading ? "animate-spin" : ""}`} />
            Refresh
          </Button>
        </div>
      </div>

      {/* Summary KPI Strip */}
      <div className="grid grid-cols-1 sm:grid-cols-4 gap-3.5">
        <div className="p-4 rounded-xl border border-zinc-800 bg-[#0d0d0d] flex items-center justify-between">
          <div>
            <div className="text-xs text-zinc-500 font-medium">Recorded Events</div>
            <div className="text-2xl font-bold text-zinc-100 mt-1">{events.length}</div>
          </div>
          <div className="h-10 w-10 rounded-lg bg-zinc-800/80 flex items-center justify-center text-zinc-400">
            <Terminal className="h-5 w-5" />
          </div>
        </div>

        <div className="p-4 rounded-xl border border-zinc-800 bg-[#0d0d0d] flex items-center justify-between">
          <div>
            <div className="text-xs text-zinc-500 font-medium">Critical Alerts</div>
            <div className="text-2xl font-bold text-rose-400 mt-1">{criticalCount}</div>
          </div>
          <div className="h-10 w-10 rounded-lg bg-rose-500/10 flex items-center justify-center text-rose-400">
            <XCircle className="h-5 w-5" />
          </div>
        </div>

        <div className="p-4 rounded-xl border border-zinc-800 bg-[#0d0d0d] flex items-center justify-between">
          <div>
            <div className="text-xs text-zinc-500 font-medium">Warnings</div>
            <div className="text-2xl font-bold text-amber-400 mt-1">{warningCount}</div>
          </div>
          <div className="h-10 w-10 rounded-lg bg-amber-500/10 flex items-center justify-center text-amber-400">
            <AlertTriangle className="h-5 w-5" />
          </div>
        </div>

        <div className="p-4 rounded-xl border border-zinc-800 bg-[#0d0d0d] flex items-center justify-between">
          <div>
            <div className="text-xs text-zinc-500 font-medium">Unread Mutations</div>
            <div className="text-2xl font-bold text-yellow-400 mt-1">{unreadCount}</div>
          </div>
          <div className="h-10 w-10 rounded-lg bg-yellow-500/10 flex items-center justify-center text-yellow-400">
            <Bell className="h-5 w-5" />
          </div>
        </div>
      </div>

      {/* Filter and Search Bar */}
      <div className="flex flex-col sm:flex-row items-center gap-3">
        <div className="relative flex-1 w-full">
          <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-zinc-500" />
          <input
            type="text"
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
            placeholder="Search events by title, description, action (e.g. vm.discovered), actor, or target ID..."
            className="w-full bg-[#0d0d0d] border border-zinc-800 rounded-lg pl-9 pr-4 py-2 text-xs text-zinc-200 placeholder-zinc-500 focus:outline-none focus:border-yellow-600 transition-colors"
          />
        </div>

        <div className="flex items-center gap-1.5 self-start sm:self-auto flex-wrap">
          {["ALL", "CRITICAL", "WARNING", "SUCCESS", "INFO"].map((sev) => (
            <button
              key={sev}
              type="button"
              onClick={() => setSeverityFilter(sev)}
              className={`px-3 py-1.5 text-xs rounded-lg font-medium transition-colors ${
                severityFilter === sev
                  ? "bg-zinc-800 text-zinc-100 border border-zinc-700"
                  : "bg-zinc-900/40 text-zinc-400 hover:text-zinc-200 border border-transparent"
              }`}
            >
              {sev === "ALL" ? "All Severities" : sev}
            </button>
          ))}
        </div>
      </div>

      {/* Events Stream List */}
      {loading && events.length === 0 ? (
        <div className="h-64 flex flex-col items-center justify-center gap-3 text-zinc-500">
          <Loader2 className="h-6 w-6 animate-spin text-yellow-500" />
          <span className="text-sm">Streaming audit events from Go ledger...</span>
        </div>
      ) : filteredItems.length === 0 ? (
        <div className="p-12 text-center text-sm text-zinc-500 border border-zinc-800 rounded-xl bg-zinc-950/20">
          No audit events found matching the selected filter.
        </div>
      ) : (
        <div className="border border-zinc-800/80 rounded-xl bg-[#0a0a0a] divide-y divide-zinc-900 overflow-hidden shadow-lg">
          {filteredItems.map(({ event: ev, synthesized, isUnread }) => {
            const isExpanded = expandedId === ev.id;
            const dateStr = new Date(ev.created_at).toLocaleString();

            return (
              <div
                key={ev.id}
                className={`p-4 transition-colors ${
                  isUnread ? "bg-yellow-950/10 hover:bg-yellow-950/20" : "hover:bg-zinc-900/30"
                }`}
              >
                <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-3">
                  <div className="flex items-start gap-3">
                    <button
                      type="button"
                      onClick={() => setExpandedId(isExpanded ? null : ev.id)}
                      className="mt-1 text-zinc-500 hover:text-zinc-300 transition-colors"
                    >
                      {isExpanded ? (
                        <ChevronDown className="h-4 w-4" />
                      ) : (
                        <ChevronRight className="h-4 w-4" />
                      )}
                    </button>

                    <div className="flex flex-col">
                      <div className="flex items-center gap-2.5 flex-wrap">
                        {getSeverityBadge(synthesized.severity)}
                        <span className="text-sm font-semibold text-zinc-100">
                          {synthesized.title}
                        </span>
                        {isUnread && (
                          <span className="h-2 w-2 rounded-full bg-yellow-500 shrink-0" title="Unread mutation" />
                        )}
                      </div>

                      <p className="text-xs text-zinc-300 mt-1">
                        {synthesized.description}
                      </p>

                      <div className="text-[11px] text-zinc-500 flex items-center gap-2 mt-1.5 flex-wrap font-mono">
                        <span className="flex items-center gap-1 font-sans">
                          <User className="h-3 w-3" /> {ev.actor}
                        </span>
                        <span>•</span>
                        <span>Action: {ev.action}</span>
                        {ev.target_id && (
                          <>
                            <span>•</span>
                            <span className="truncate max-w-[200px]">Target: {ev.target_id}</span>
                          </>
                        )}
                      </div>
                    </div>
                  </div>

                  <div className="flex items-center gap-3 self-end sm:self-center shrink-0">
                    {synthesized.vmId && (
                      <Button
                        variant="outline"
                        size="sm"
                        onClick={() => setSelectedVmId(synthesized.vmId!)}
                        className="h-7 text-xs border-zinc-800 bg-zinc-900/60 text-yellow-400 hover:text-yellow-300 hover:bg-zinc-800 gap-1"
                      >
                        <ExternalLink className="h-3 w-3" />
                        Inspect VM
                      </Button>
                    )}

                    <div className="flex items-center gap-1.5 text-xs text-zinc-500 font-mono">
                      <Clock className="h-3.5 w-3.5" />
                      <span>{dateStr}</span>
                    </div>
                  </div>
                </div>

                {/* Expanded Details JSON Drawer */}
                {isExpanded && (
                  <div className="mt-3.5 pt-3.5 border-t border-zinc-900 space-y-2">
                    <div className="text-[11px] font-semibold text-zinc-400 uppercase tracking-wider">
                      Event Metadata & Payload Details
                    </div>
                    <pre className="p-3 rounded-lg bg-zinc-950 border border-zinc-850 text-emerald-400 font-mono text-xs overflow-x-auto">
                      {JSON.stringify(
                        {
                          event_id: ev.id,
                          action: ev.action,
                          actor: ev.actor,
                          target_id: ev.target_id,
                          outcome: ev.outcome,
                          request_id: ev.request_id || "N/A",
                          timestamp: ev.created_at,
                          details: ev.details,
                        },
                        null,
                        2
                      )}
                    </pre>
                  </div>
                )}
              </div>
            );
          })}
        </div>
      )}

      {/* Actionable VM Detail Sheet */}
      <VMDetailSheet
        vmId={selectedVmId}
        onClose={() => setSelectedVmId(null)}
      />
    </div>
  );
}
