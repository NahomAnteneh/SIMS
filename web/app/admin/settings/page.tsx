"use client";

import React, { useState, useEffect } from "react";
import { 
  Settings, 
  User, 
  Shield, 
  Server, 
  Database, 
  CheckCircle2, 
  Lock, 
  Key, 
  Globe, 
  Loader2,
  Cpu
} from "lucide-react";
import { simsApi } from "@/lib/api/client";
import { UserResponse } from "@/lib/api/types";

export default function SettingsPage() {
  const [user, setUser] = useState<UserResponse | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    async function loadSettings() {
      setLoading(true);
      try {
        const me = await simsApi.getMe();
        setUser(me);
      } catch (err) {
        console.error("Failed to load user settings", err);
      } finally {
        setLoading(false);
      }
    }

    loadSettings();
  }, []);

  return (
    <div className="space-y-4 w-full">
      {/* Header */}
      <div className="flex items-center justify-between pb-2 border-b border-zinc-800/80">
        <div className="flex items-center gap-2">
          <Settings className="h-5 w-5 text-yellow-500" />
          <h1 className="text-xl font-bold tracking-tight text-zinc-100">
            System Settings & Platform Diagnostics
          </h1>
        </div>
      </div>

      {loading ? (
        <div className="h-64 flex flex-col items-center justify-center gap-3 text-zinc-500">
          <Loader2 className="h-6 w-6 animate-spin text-yellow-500" />
          <span className="text-sm">Reading platform diagnostic parameters...</span>
        </div>
      ) : (
        <div className="space-y-4 w-full">
          {/* User Session Card */}
          <div className="border border-zinc-800 bg-[#0a0a0a] p-4 rounded-none space-y-3">
            <h3 className="text-xs font-mono font-bold uppercase tracking-wider text-zinc-200 flex items-center gap-2">
              <User className="h-3.5 w-3.5 text-yellow-500" />
              Active Operator Identity
            </h3>

            <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-3 text-xs">
              <div className="p-3 rounded-none bg-zinc-900/60 border border-zinc-800">
                <div className="text-[11px] font-mono text-zinc-500 mb-1 uppercase">Username</div>
                <div className="font-mono text-zinc-200 font-semibold text-sm">
                  {user?.username || "admin"}
                </div>
              </div>

              <div className="p-3 rounded-none bg-zinc-900/60 border border-zinc-800">
                <div className="text-[11px] font-mono text-zinc-500 mb-1 uppercase">Role / Privilege</div>
                <div className="flex items-center gap-2">
                  <span className="capitalize font-semibold text-zinc-200">
                    {user?.role || "admin"}
                  </span>
                  <span className="px-1.5 py-0.2 rounded-none text-[10px] font-mono font-bold uppercase tracking-wider bg-yellow-600/20 text-yellow-500 border border-yellow-600/30">
                    Full Access
                  </span>
                </div>
              </div>

              <div className="p-3 rounded-none bg-zinc-900/60 border border-zinc-800">
                <div className="text-[11px] font-mono text-zinc-500 mb-1 uppercase">Email</div>
                <div className="text-zinc-300 font-mono">
                  {user?.email || "admin@sims.local"}
                </div>
              </div>

              <div className="p-3 rounded-none bg-zinc-900/60 border border-zinc-800">
                <div className="text-[11px] font-mono text-zinc-500 mb-1 uppercase">Tenant Scope</div>
                <div className="text-emerald-400 font-medium flex items-center gap-1.5">
                  <CheckCircle2 className="h-3.5 w-3.5" />
                  Global Unrestricted
                </div>
              </div>
            </div>
          </div>

          {/* Engine & Service Diagnostics */}
          <div className="border border-zinc-800 bg-[#0a0a0a] p-4 rounded-none space-y-3">
            <h3 className="text-xs font-mono font-bold uppercase tracking-wider text-zinc-200 flex items-center gap-2">
              <Server className="h-3.5 w-3.5 text-blue-400" />
              SIMS Platform Architecture & Port Topology
            </h3>

            <div className="grid grid-cols-1 md:grid-cols-3 gap-3 text-xs">
              <div className="p-3 rounded-none bg-zinc-900/40 border border-zinc-800 flex flex-col justify-between gap-3">
                <div className="flex items-center gap-2.5">
                  <Globe className="h-4 w-4 text-zinc-400 shrink-0" />
                  <div>
                    <div className="font-semibold text-zinc-200">Query API Service</div>
                    <div className="text-zinc-500 text-[11px] font-mono">Go net/http + pgx/v5 REST</div>
                  </div>
                </div>
                <div className="flex items-center gap-2 font-mono text-zinc-300 pt-2 border-t border-zinc-800/80">
                  <span className="w-2 h-2 rounded-none bg-emerald-500"></span>
                  Port 8080 (Healthy)
                </div>
              </div>

              <div className="p-3 rounded-none bg-zinc-900/40 border border-zinc-800 flex flex-col justify-between gap-3">
                <div className="flex items-center gap-2.5">
                  <Database className="h-4 w-4 text-zinc-400 shrink-0" />
                  <div>
                    <div className="font-semibold text-zinc-200">Inventory Storage Engine</div>
                    <div className="text-zinc-500 text-[11px] font-mono">PostgreSQL 18 (ltree/multirange)</div>
                  </div>
                </div>
                <div className="flex items-center gap-2 font-mono text-zinc-300 pt-2 border-t border-zinc-800/80">
                  <span className="w-2 h-2 rounded-none bg-emerald-500"></span>
                  Port 5433 (Healthy)
                </div>
              </div>

              <div className="p-3 rounded-none bg-zinc-900/40 border border-zinc-800 flex flex-col justify-between gap-3">
                <div className="flex items-center gap-2.5">
                  <Cpu className="h-4 w-4 text-zinc-400 shrink-0" />
                  <div>
                    <div className="font-semibold text-zinc-200">PropertyCollector Synchronizer</div>
                    <div className="text-zinc-500 text-[11px] font-mono">WaitForUpdatesEx agent</div>
                  </div>
                </div>
                <div className="flex items-center gap-2 font-mono text-zinc-300 pt-2 border-t border-zinc-800/80">
                  <span className="w-2 h-2 rounded-none bg-emerald-500"></span>
                  Port 8081 (Streaming)
                </div>
              </div>
            </div>
          </div>

          {/* Security & Access Policies */}
          <div className="border border-zinc-800 bg-[#0a0a0a] p-4 rounded-none space-y-3">
            <h3 className="text-xs font-mono font-bold uppercase tracking-wider text-zinc-200 flex items-center gap-2">
              <Shield className="h-3.5 w-3.5 text-emerald-400" />
              Security & Policy Guardrails
            </h3>

            <div className="grid grid-cols-1 sm:grid-cols-2 gap-3 text-xs">
              <div className="p-3 rounded-none bg-zinc-900/40 border border-zinc-800">
                <div className="text-zinc-500 mb-1 flex items-center gap-1.5 font-mono text-[11px] uppercase">
                  <Lock className="h-3.5 w-3.5 text-zinc-400" /> Session Lifespan
                </div>
                <div className="font-mono text-zinc-200 font-medium">24 Hours (HMAC-SHA256 JWT)</div>
              </div>

              <div className="p-3 rounded-none bg-zinc-900/40 border border-zinc-800">
                <div className="text-zinc-500 mb-1 flex items-center gap-1.5 font-mono text-[11px] uppercase">
                  <Key className="h-3.5 w-3.5 text-zinc-400" /> Credential Hashing
                </div>
                <div className="font-mono text-zinc-200 font-medium">bcrypt (Cost Factor 10)</div>
              </div>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
