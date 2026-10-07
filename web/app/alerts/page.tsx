"use client";

import React, { useState, useEffect, useCallback } from "react";
import { 
  Bell, 
  ShieldCheck, 
  AlertTriangle, 
  XCircle, 
  RefreshCw, 
  Search, 
  Loader2, 
  Clock, 
  User, 
  Terminal,
  CheckCircle2,
  ChevronDown,
  ChevronRight
} from "lucide-react";
import { simsApi } from "@/lib/api/client";
import { AuditEventItem } from "@/lib/api/types";
import { Button } from "@/components/ui/button";

export default function AlertsPage() {
  const [events, setEvents] = useState<AuditEventItem[]>([]);
  const [loading, setLoading] = useState(true);
  const [filterOutcome, setFilterOutcome] = useState<string>("ALL");
  const [searchQuery, setSearchQuery] = useState("");
  const [expandedId, setExpandedId] = useState<string | null>(null);

  const loadEvents = useCallback(async () => {
    setLoading(true);
    try {
      const data = await simsApi.listAuditEvents();
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

  const filteredEvents = events.filter((ev) => {
    const outcomeMatch =
      filterOutcome === "ALL" ||
      ev.outcome?.toUpperCase() === filterOutcome.toUpperCase();

    const q = searchQuery.toLowerCase();
    const queryMatch =
      !q ||
      ev.action?.toLowerCase().includes(q) ||
      ev.actor?.toLowerCase().includes(q) ||
      ev.target_id?.toLowerCase().includes(q);

    return outcomeMatch && queryMatch;
  });

  const successCount = events.filter((e) => e.outcome?.toUpperCase() === "SUCCESS").length;
  const failureCount = events.filter((e) => e.outcome?.toUpperCase() === "FAILURE").length;

  return (
    <div className="space-y-6 max-w-7xl mx-auto p-2 md:p-4">
      {/* Page Header */}
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4">
        <div>
          <h1 className="text-2xl font-semibold text-zinc-100 flex items-center gap-2.5">
            <Bell className="h-6 w-6 text-yellow-500" />
            Alerts & Audit Event Log
          </h1>
          <p className="text-sm text-zinc-500 mt-1">
            Immutable audit ledger of security logins, vCenter syncs, resource mutations, and operational tasks.
          </p>
        </div>

        <Button
          variant="outline"
          size="sm"
          onClick={loadEvents}
          disabled={loading}
          className="border-zinc-800 bg-zinc-900/50 text-zinc-300 hover:text-white hover:bg-zinc-800 gap-1.5 h-8 w-fit"
        >
          <RefreshCw className={`h-3.5 w-3.5 ${loading ? "animate-spin" : ""}`} />
          Refresh Stream
        </Button>
      </div>

      {/* Summary KPI Strip */}
      <div className="grid grid-cols-1 sm:grid-cols-3 gap-4">
        <div className="p-4 rounded-xl border border-zinc-800 bg-[#0d0d0d] flex items-center justify-between">
          <div>
            <div className="text-xs text-zinc-500 font-medium">Total Recorded Events</div>
            <div className="text-2xl font-bold text-zinc-100 mt-1">{events.length}</div>
          </div>
          <div className="h-10 w-10 rounded-lg bg-zinc-800/80 flex items-center justify-center text-zinc-400">
            <Terminal className="h-5 w-5" />
          </div>
        </div>

        <div className="p-4 rounded-xl border border-zinc-800 bg-[#0d0d0d] flex items-center justify-between">
          <div>
            <div className="text-xs text-zinc-500 font-medium">Successful Operations</div>
            <div className="text-2xl font-bold text-emerald-400 mt-1">{successCount}</div>
          </div>
          <div className="h-10 w-10 rounded-lg bg-emerald-500/10 flex items-center justify-center text-emerald-400">
            <CheckCircle2 className="h-5 w-5" />
          </div>
        </div>

        <div className="p-4 rounded-xl border border-zinc-800 bg-[#0d0d0d] flex items-center justify-between">
          <div>
            <div className="text-xs text-zinc-500 font-medium">Failed / Warning Events</div>
            <div className="text-2xl font-bold text-rose-400 mt-1">{failureCount}</div>
          </div>
          <div className="h-10 w-10 rounded-lg bg-rose-500/10 flex items-center justify-center text-rose-400">
            <XCircle className="h-5 w-5" />
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
            placeholder="Search events by action (e.g. auth.login, vcenter.resync), actor, or target ID..."
            className="w-full bg-[#0d0d0d] border border-zinc-800 rounded-lg pl-9 pr-4 py-2 text-xs text-zinc-200 placeholder-zinc-500 focus:outline-none focus:border-yellow-600 transition-colors"
          />
        </div>

        <div className="flex items-center gap-1.5 self-start sm:self-auto">
          {["ALL", "SUCCESS", "FAILURE"].map((out) => (
            <button
              key={out}
              onClick={() => setFilterOutcome(out)}
              className={`px-3 py-1.5 text-xs rounded-lg font-medium transition-colors ${
                filterOutcome === out
                  ? "bg-zinc-800 text-zinc-100 border border-zinc-700"
                  : "bg-zinc-900/40 text-zinc-400 hover:text-zinc-200 border border-transparent"
              }`}
            >
              {out === "ALL" ? "All Outcomes" : out}
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
      ) : filteredEvents.length === 0 ? (
        <div className="p-12 text-center text-sm text-zinc-500 border border-zinc-800 rounded-xl bg-zinc-950/20">
          No audit events found matching the selected filter.
        </div>
      ) : (
        <div className="border border-zinc-800/80 rounded-xl bg-[#0a0a0a] divide-y divide-zinc-900 overflow-hidden shadow-lg">
          {filteredEvents.map((ev) => {
            const isExpanded = expandedId === ev.id;
            const isSuccess = ev.outcome?.toUpperCase() === "SUCCESS" || ev.outcome?.toLowerCase() === "accepted";
            const dateStr = new Date(ev.created_at).toLocaleString();

            return (
              <div key={ev.id} className="p-4 hover:bg-zinc-900/30 transition-colors">
                <div 
                  className="flex flex-col sm:flex-row sm:items-center justify-between gap-3 cursor-pointer"
                  onClick={() => setExpandedId(isExpanded ? null : ev.id)}
                >
                  <div className="flex items-center gap-3">
                    <button className="text-zinc-500 hover:text-zinc-300">
                      {isExpanded ? <ChevronDown className="h-4 w-4" /> : <ChevronRight className="h-4 w-4" />}
                    </button>

                    <div className="flex flex-col">
                      <div className="flex items-center gap-2">
                        <span className="font-mono text-xs font-semibold text-zinc-100">
                          {ev.action}
                        </span>
                        <span
                          className={`inline-flex items-center px-2 py-0.5 rounded-full text-[10px] font-bold uppercase tracking-wider ${
                            isSuccess
                              ? "bg-emerald-500/15 text-emerald-400 border border-emerald-500/30"
                              : "bg-rose-500/15 text-rose-400 border border-rose-500/30"
                          }`}
                        >
                          {ev.outcome}
                        </span>
                      </div>
                      <div className="text-[11px] text-zinc-500 flex items-center gap-2 mt-1">
                        <span className="flex items-center gap-1">
                          <User className="h-3 w-3" /> {ev.actor}
                        </span>
                        <span>•</span>
                        <span className="truncate max-w-[200px]">Target: {ev.target_id || "system"}</span>
                      </div>
                    </div>
                  </div>

                  <div className="flex items-center gap-2 text-xs text-zinc-500 font-mono">
                    <Clock className="h-3.5 w-3.5" />
                    <span>{dateStr}</span>
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
    </div>
  );
}
