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
    <div className="space-y-6 max-w-5xl mx-auto p-2 md:p-4">
      {/* Header */}
      <div>
        <h1 className="text-2xl font-semibold text-zinc-100 flex items-center gap-2.5">
          <Settings className="h-6 w-6 text-yellow-500" />
          System Settings & Platform Diagnostics
        </h1>
        <p className="text-sm text-zinc-500 mt-1">
          Active session identity, security parameters, API Gateway configuration, and engine diagnostics.
        </p>
      </div>

      {loading ? (
        <div className="h-64 flex flex-col items-center justify-center gap-3 text-zinc-500">
          <Loader2 className="h-6 w-6 animate-spin text-yellow-500" />
          <span className="text-sm">Reading platform diagnostic parameters...</span>
        </div>
      ) : (
        <div className="space-y-6">
          {/* User Session Card */}
          <div className="border border-zinc-800/80 rounded-xl bg-[#0a0a0a] p-5 shadow-lg space-y-4">
            <h3 className="text-sm font-semibold text-zinc-200 flex items-center gap-2">
              <User className="h-4 w-4 text-yellow-500" />
              Active Operator Identity
            </h3>

            <div className="grid grid-cols-1 sm:grid-cols-2 gap-4 text-xs">
              <div className="p-3 rounded-lg bg-zinc-900/50 border border-zinc-850">
                <div className="text-zinc-500 mb-1">Username</div>
                <div className="font-mono text-zinc-200 font-semibold text-sm">
                  {user?.username || "admin"}
                </div>
              </div>

              <div className="p-3 rounded-lg bg-zinc-900/50 border border-zinc-850">
                <div className="text-zinc-500 mb-1">Role / Privilege Level</div>
                <div className="flex items-center gap-2">
                  <span className="capitalize font-semibold text-zinc-200">
                    {user?.role || "admin"}
                  </span>
                  <span className="px-2 py-0.5 rounded text-[10px] font-bold uppercase tracking-wider bg-yellow-600/20 text-yellow-500 border border-yellow-600/30">
                    Full Access
                  </span>
                </div>
              </div>

              <div className="p-3 rounded-lg bg-zinc-900/50 border border-zinc-850">
                <div className="text-zinc-500 mb-1">Email</div>
                <div className="text-zinc-300 font-mono">
                  {user?.email || "admin@sims.local"}
                </div>
              </div>

              <div className="p-3 rounded-lg bg-zinc-900/50 border border-zinc-850">
                <div className="text-zinc-500 mb-1">Tenant Scope Restriction</div>
                <div className="text-emerald-400 font-medium flex items-center gap-1.5">
                  <CheckCircle2 className="h-3.5 w-3.5" />
                  Unrestricted Global Inventory
                </div>
              </div>
            </div>
          </div>

          {/* Engine & Service Diagnostics */}
          <div className="border border-zinc-800/80 rounded-xl bg-[#0a0a0a] p-5 shadow-lg space-y-4">
            <h3 className="text-sm font-semibold text-zinc-200 flex items-center gap-2">
              <Server className="h-4 w-4 text-blue-400" />
              SIMS Platform Architecture
            </h3>

            <div className="space-y-3 text-xs">
              <div className="flex items-center justify-between p-3 rounded-lg bg-zinc-900/40 border border-zinc-850">
                <div className="flex items-center gap-2.5">
                  <Globe className="h-4 w-4 text-zinc-400" />
                  <div>
                    <div className="font-medium text-zinc-200">Query API Service</div>
                    <div className="text-zinc-500 text-[11px]">Go net/http + pgx/v5 REST Gateway</div>
                  </div>
                </div>
                <div className="flex items-center gap-2 font-mono text-zinc-300">
                  <span className="w-2 h-2 rounded-full bg-emerald-500"></span>
                  Port 8080 (Healthy)
                </div>
              </div>

              <div className="flex items-center justify-between p-3 rounded-lg bg-zinc-900/40 border border-zinc-850">
                <div className="flex items-center gap-2.5">
                  <Database className="h-4 w-4 text-zinc-400" />
                  <div>
                    <div className="font-medium text-zinc-200">Inventory Storage Engine</div>
                    <div className="text-zinc-500 text-[11px]">PostgreSQL 18.6 with ltree & multirange extensions</div>
                  </div>
                </div>
                <div className="flex items-center gap-2 font-mono text-zinc-300">
                  <span className="w-2 h-2 rounded-full bg-emerald-500"></span>
                  Port 5433 (Healthy)
                </div>
              </div>

              <div className="flex items-center justify-between p-3 rounded-lg bg-zinc-900/40 border border-zinc-850">
                <div className="flex items-center gap-2.5">
                  <Cpu className="h-4 w-4 text-zinc-400" />
                  <div>
                    <div className="font-medium text-zinc-200">PropertyCollector Ingestion Agent</div>
                    <div className="text-zinc-500 text-[11px]">Continuous streaming WaitForUpdatesEx synchronizer</div>
                  </div>
                </div>
                <div className="flex items-center gap-2 font-mono text-zinc-300">
                  <span className="w-2 h-2 rounded-full bg-emerald-500"></span>
                  Port 8081 (Streaming)
                </div>
              </div>
            </div>
          </div>

          {/* Security & Access Policies */}
          <div className="border border-zinc-800/80 rounded-xl bg-[#0a0a0a] p-5 shadow-lg space-y-4">
            <h3 className="text-sm font-semibold text-zinc-200 flex items-center gap-2">
              <Shield className="h-4 w-4 text-emerald-400" />
              Security & Policy Guardrails
            </h3>

            <div className="grid grid-cols-1 sm:grid-cols-2 gap-4 text-xs">
              <div className="p-3 rounded-lg bg-zinc-900/40 border border-zinc-850">
                <div className="text-zinc-500 mb-1 flex items-center gap-1.5">
                  <Lock className="h-3.5 w-3.5 text-zinc-400" /> JWT Token Session Lifespan
                </div>
                <div className="font-mono text-zinc-200 font-medium">24 Hours (HMAC-SHA256)</div>
              </div>

              <div className="p-3 rounded-lg bg-zinc-900/40 border border-zinc-850">
                <div className="text-zinc-500 mb-1 flex items-center gap-1.5">
                  <Key className="h-3.5 w-3.5 text-zinc-400" /> Password Security Hashing
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
