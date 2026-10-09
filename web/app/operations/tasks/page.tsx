"use client";

import React, { useState, useEffect, useCallback } from "react";
import { 
  Activity, 
  CheckCircle2, 
  RefreshCw, 
  Clock, 
  User, 
  Server, 
  Loader2, 
  Layers,
  Shield,
  RotateCw
} from "lucide-react";
import { simsApi } from "@/lib/api/client";
import { AuditEventItem } from "@/lib/api/types";
import { Button } from "@/components/ui/button";

export default function TasksPage() {
  const [tasks, setTasks] = useState<AuditEventItem[]>([]);
  const [loading, setLoading] = useState(true);

  const loadTasks = useCallback(async () => {
    setLoading(true);
    try {
      const allEvents = await simsApi.listAuditEvents();
      // Filter to operational and management tasks
      const opTasks = allEvents.filter((ev) => 
        ev.action.startsWith("vcenter.") || 
        ev.action.startsWith("user.") ||
        ev.action.startsWith("collector.")
      );
      setTasks(opTasks);
    } catch (err) {
      console.error("Failed to load tasks", err);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    loadTasks();
  }, [loadTasks]);

  return (
    <div className="space-y-6 max-w-7xl mx-auto p-2 md:p-4">
      {/* Page Header */}
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4">
        <div>
          <h1 className="text-2xl font-semibold text-zinc-100 flex items-center gap-2.5">
            <Activity className="h-6 w-6 text-yellow-500" />
            Operational Task Console
          </h1>
          <p className="text-sm text-zinc-500 mt-1">
            Real-time tracking of vCenter synchronization, collector jobs, and management tasks.
          </p>
        </div>

        <Button
          variant="outline"
          size="sm"
          onClick={loadTasks}
          disabled={loading}
          className="border-zinc-800 bg-zinc-900/50 text-zinc-300 hover:text-white hover:bg-zinc-800 gap-1.5 h-8 w-fit"
        >
          <RefreshCw className={`h-3.5 w-3.5 ${loading ? "animate-spin" : ""}`} />
          Refresh Tasks
        </Button>
      </div>

      {loading && tasks.length === 0 ? (
        <div className="h-64 flex flex-col items-center justify-center gap-3 text-zinc-500">
          <Loader2 className="h-6 w-6 animate-spin text-yellow-500" />
          <span className="text-sm">Querying task execution history...</span>
        </div>
      ) : tasks.length === 0 ? (
        <div className="p-12 text-center text-sm text-zinc-500 border border-zinc-800 rounded-xl bg-zinc-950/20">
          No administrative or background tasks recorded yet. Trigger a vCenter resync to view live progress.
        </div>
      ) : (
        <div className="border border-zinc-800/80 rounded-xl bg-[#0a0a0a] overflow-hidden shadow-lg">
          <table className="w-full text-left border-collapse text-xs">
            <thead>
              <tr className="border-b border-zinc-800/80 bg-zinc-900/40 text-zinc-400 font-semibold uppercase tracking-wider text-[11px]">
                <th className="py-3 px-4">Task Operation</th>
                <th className="py-3 px-4">Target Entity</th>
                <th className="py-3 px-4">Triggered By</th>
                <th className="py-3 px-4">State</th>
                <th className="py-3 px-4">Timestamp</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-zinc-900">
              {tasks.map((task) => {
                const isSuccess = task.outcome?.toUpperCase() === "SUCCESS" || task.outcome?.toLowerCase() === "accepted";
                const dateStr = new Date(task.created_at).toLocaleString();

                return (
                  <tr key={task.id} className="hover:bg-zinc-900/30 transition-colors">
                    <td className="py-3 px-4">
                      <div className="flex items-center gap-2 font-mono font-medium text-zinc-200">
                        {task.action.includes("resync") ? (
                          <RotateCw className="h-4 w-4 text-yellow-500 shrink-0" />
                        ) : task.action.includes("vcenter") ? (
                          <Server className="h-4 w-4 text-blue-400 shrink-0" />
                        ) : (
                          <Shield className="h-4 w-4 text-purple-400 shrink-0" />
                        )}
                        <span>{task.action}</span>
                      </div>
                    </td>

                    <td className="py-3 px-4 text-zinc-400 font-mono">
                      {task.target_id || "sims-core"}
                    </td>

                    <td className="py-3 px-4 text-zinc-300">
                      <div className="flex items-center gap-1.5">
                        <User className="h-3 w-3 text-zinc-500" />
                        <span>{task.actor}</span>
                      </div>
                    </td>

                    <td className="py-3 px-4">
                      <span
                        className={`inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-[10px] font-bold uppercase tracking-wider ${
                          isSuccess
                            ? "bg-emerald-500/15 text-emerald-400 border border-emerald-500/30"
                            : "bg-rose-500/15 text-rose-400 border border-rose-500/30"
                        }`}
                      >
                        <span className={`w-1.5 h-1.5 rounded-full ${isSuccess ? "bg-emerald-400" : "bg-rose-400"}`} />
                        {task.outcome}
                      </span>
                    </td>

                    <td className="py-3 px-4 text-zinc-500 font-mono">
                      {dateStr}
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}
