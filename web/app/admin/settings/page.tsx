"use client";

import React, { useState, useEffect, useCallback } from "react";
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
  Cpu,
  UserPlus,
  Users,
  Trash2,
  Ban,
  Check,
  AlertCircle,
  RefreshCw,
} from "lucide-react";
import { simsApi } from "@/lib/api/client";
import { UserResponse } from "@/lib/api/types";
import { Button } from "@/components/ui/button";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { ConfirmModal, AlertModal, ModalPopup } from "@/components/ui/modal-popup";

export default function SettingsPage() {
  const [currentUser, setCurrentUser] = useState<UserResponse | null>(null);
  const [loading, setLoading] = useState(true);

  // User Management State (Admins only)
  const [users, setUsers] = useState<UserResponse[]>([]);
  const [usersLoading, setUsersLoading] = useState(false);
  const [isAddUserOpen, setIsAddUserOpen] = useState(false);
  const [userFormData, setUserFormData] = useState({
    username: "",
    password: "",
    email: "",
    role: "viewer",
  });
  const [addUserSubmitting, setAddUserSubmitting] = useState(false);
  const [addUserError, setAddUserError] = useState<string | null>(null);

  // Action modals
  const [deleteTarget, setDeleteTarget] = useState<{ id: string; username: string } | null>(null);
  const [deleteLoading, setDeleteLoading] = useState(false);
  const [alertInfo, setAlertInfo] = useState<{
    isOpen: boolean;
    title: string;
    message: string;
    details?: string;
    type?: "error" | "success" | "info";
  } | null>(null);

  const isAdmin =
    currentUser?.role === "admin" || currentUser?.role === "inventory-admin";

  const loadUsers = useCallback(async () => {
    setUsersLoading(true);
    try {
      const resp = await simsApi.listUsers();
      setUsers(resp || []);
    } catch (err: any) {
      console.error("Failed to load user list:", err);
    } finally {
      setUsersLoading(false);
    }
  }, []);

  useEffect(() => {
    async function loadSettings() {
      setLoading(true);
      try {
        const me = await simsApi.getMe();
        setCurrentUser(me);
        if (me.role === "admin" || me.role === "inventory-admin") {
          const resp = await simsApi.listUsers();
          setUsers(resp || []);
        }
      } catch (err) {
        console.error("Failed to load user settings", err);
      } finally {
        setLoading(false);
      }
    }

    loadSettings();
  }, []);

  const handleCreateUser = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!userFormData.username || !userFormData.password) {
      setAddUserError("Username and password are required.");
      return;
    }
    if (userFormData.password.length < 6) {
      setAddUserError("Password must be at least 6 characters.");
      return;
    }

    setAddUserSubmitting(true);
    setAddUserError(null);
    try {
      await simsApi.createUser({
        username: userFormData.username.trim(),
        password: userFormData.password,
        email: userFormData.email.trim() || undefined,
        role: userFormData.role,
      });

      setIsAddUserOpen(false);
      setUserFormData({ username: "", password: "", email: "", role: "viewer" });
      await loadUsers();
      setAlertInfo({
        isOpen: true,
        title: "User Created",
        message: `User '${userFormData.username}' has been successfully provisioned.`,
        type: "success",
      });
    } catch (err: any) {
      setAddUserError(err?.message || "Failed to create user.");
    } finally {
      setAddUserSubmitting(false);
    }
  };

  const handleRoleChange = async (userId: string, newRole: string) => {
    try {
      await simsApi.updateUser(userId, { role: newRole });
      await loadUsers();
    } catch (err: any) {
      setAlertInfo({
        isOpen: true,
        title: "Role Update Failed",
        message: "Failed to update user role privileges.",
        details: err?.message || String(err),
        type: "error",
      });
    }
  };

  const handleToggleSuspend = async (user: UserResponse) => {
    const isSuspended = user.role === "suspended";
    const targetRole = isSuspended ? "operator" : "suspended";
    try {
      await simsApi.updateUser(user.id, { role: targetRole });
      await loadUsers();
      setAlertInfo({
        isOpen: true,
        title: isSuspended ? "User Account Activated" : "User Account Suspended",
        message: isSuspended
          ? `User '${user.username}' is now active with role '${targetRole}'.`
          : `User '${user.username}' has been suspended from logging in.`,
        type: isSuspended ? "success" : "info",
      });
    } catch (err: any) {
      setAlertInfo({
        isOpen: true,
        title: "Suspension Status Update Failed",
        message: "Failed to change user suspension status.",
        details: err?.message || String(err),
        type: "error",
      });
    }
  };

  const handleConfirmDelete = async () => {
    if (!deleteTarget) return;
    setDeleteLoading(true);
    try {
      await simsApi.deleteUser(deleteTarget.id);
      setDeleteTarget(null);
      await loadUsers();
      setAlertInfo({
        isOpen: true,
        title: "User Deleted",
        message: `User '${deleteTarget.username}' was permanently removed.`,
        type: "success",
      });
    } catch (err: any) {
      setAlertInfo({
        isOpen: true,
        title: "Deletion Failed",
        message: `Failed to delete user '${deleteTarget.username}'.`,
        details: err?.message || String(err),
        type: "error",
      });
    } finally {
      setDeleteLoading(false);
    }
  };

  return (
    <div className="space-y-4 w-full">
      {/* Header */}
      <div className="flex items-center justify-between pb-2 border-b border-zinc-800/80">
        <div className="flex items-center gap-2.5">
          <div className="p-1.5 bg-yellow-500/10 border border-yellow-500/20 text-yellow-500">
            <Settings className="h-4 w-4" />
          </div>
          <div>
            <h1 className="text-xl font-bold tracking-tight text-zinc-100">
              System Settings & Platform Governance
            </h1>
            <p className="text-[11px] font-mono text-zinc-500 mt-0.5">
              Identity parameters, RBAC user administration, and query engine diagnostics
            </p>
          </div>
        </div>
      </div>

      {loading ? (
        <div className="h-64 flex flex-col items-center justify-center gap-3 text-zinc-500">
          <Loader2 className="h-6 w-6 animate-spin text-yellow-500" />
          <span className="text-xs font-mono">Loading platform configuration parameters...</span>
        </div>
      ) : (
        <Tabs defaultValue={isAdmin ? "users" : "diagnostics"} className="w-full space-y-4">
          <TabsList className="bg-zinc-900 border border-zinc-800 p-0.5 rounded-none">
            {isAdmin && (
              <TabsTrigger
                value="users"
                className="rounded-none text-xs flex items-center gap-2 data-[state=active]:bg-zinc-800 data-[state=active]:text-yellow-400"
              >
                <Users className="h-3.5 w-3.5" />
                <span>User Management & RBAC</span>
                <span className="px-1.5 py-0.2 bg-yellow-500/20 text-yellow-400 text-[10px] font-mono font-bold">
                  {users.length}
                </span>
              </TabsTrigger>
            )}
            <TabsTrigger
              value="diagnostics"
              className="rounded-none text-xs flex items-center gap-2 data-[state=active]:bg-zinc-800 data-[state=active]:text-yellow-400"
            >
              <Server className="h-3.5 w-3.5" />
              <span>Platform Diagnostics & Identity</span>
            </TabsTrigger>
          </TabsList>

          {/* User Management Tab (Admins Only) */}
          {isAdmin && (
            <TabsContent value="users" className="space-y-4 m-0">
              <div className="border border-zinc-800 bg-[#0a0a0a] p-4 rounded-none space-y-4">
                <div className="flex flex-col sm:flex-row sm:items-center justify-between pb-3 border-b border-zinc-850 gap-2">
                  <div>
                    <h3 className="text-xs font-mono font-bold uppercase tracking-wider text-zinc-100 flex items-center gap-2">
                      <Shield className="h-3.5 w-3.5 text-yellow-500" />
                      Platform Operators & RBAC Governance
                    </h3>
                    <p className="text-[11px] text-zinc-400 mt-0.5">
                      Provision user credentials, assign role privileges, and suspend unauthorized accounts.
                    </p>
                  </div>

                  <div className="flex items-center gap-2">
                    <Button
                      variant="outline"
                      size="sm"
                      onClick={loadUsers}
                      disabled={usersLoading}
                      className="h-8 text-xs rounded-none border-zinc-800 bg-zinc-900/40 text-zinc-300 hover:text-white"
                    >
                      <RefreshCw className={`h-3 w-3 mr-1.5 ${usersLoading ? "animate-spin" : ""}`} />
                      Refresh
                    </Button>
                    <Button
                      size="sm"
                      onClick={() => {
                        setAddUserError(null);
                        setIsAddUserOpen(true);
                      }}
                      className="h-8 text-xs rounded-none bg-yellow-600 hover:bg-yellow-500 text-black font-semibold gap-1.5"
                    >
                      <UserPlus className="h-3.5 w-3.5" />
                      Add User
                    </Button>
                  </div>
                </div>

                {usersLoading && users.length === 0 ? (
                  <div className="h-40 flex flex-col items-center justify-center gap-2 text-zinc-500">
                    <Loader2 className="h-5 w-5 animate-spin text-yellow-500" />
                    <span className="text-xs font-mono">Reading user directory...</span>
                  </div>
                ) : (
                  <div className="overflow-x-auto border border-zinc-800/80">
                    <table className="w-full text-left text-xs font-mono text-zinc-300">
                      <thead className="border-b border-zinc-800 bg-zinc-950 text-[10px] text-zinc-500 uppercase tracking-wider font-semibold">
                        <tr>
                          <th className="py-2.5 px-3">Username / Email</th>
                          <th className="py-2.5 px-3">Role Privilege</th>
                          <th className="py-2.5 px-3">Status</th>
                          <th className="py-2.5 px-3">Tenant Scope</th>
                          <th className="py-2.5 px-3">Created</th>
                          <th className="py-2.5 px-3 text-right">Actions</th>
                        </tr>
                      </thead>
                      <tbody className="divide-y divide-zinc-850">
                        {users.map((u) => {
                          const isSelf = currentUser?.id === u.id;
                          const isSuspended = u.role === "suspended";
                          const isUserAdmin = u.role === "admin" || u.role === "inventory-admin";

                          return (
                            <tr key={u.id} className="hover:bg-zinc-900/30 transition-colors">
                              {/* Username */}
                              <td className="py-3 px-3">
                                <div className="flex items-center gap-2.5">
                                  <div className="h-6 w-6 bg-zinc-900 border border-zinc-800 flex items-center justify-center text-[10px] text-yellow-500 font-bold uppercase shrink-0">
                                    {u.username.slice(0, 2)}
                                  </div>
                                  <div className="flex flex-col">
                                    <span className="font-semibold text-zinc-100 flex items-center gap-1.5">
                                      {u.username}
                                      {isSelf && (
                                        <span className="px-1 py-0 bg-zinc-800 text-zinc-400 text-[9px] uppercase tracking-wider">
                                          you
                                        </span>
                                      )}
                                    </span>
                                    {u.email && (
                                      <span className="text-[10px] text-zinc-500 font-sans">
                                        {u.email}
                                      </span>
                                    )}
                                  </div>
                                </div>
                              </td>

                              {/* Role Selector */}
                              <td className="py-3 px-3">
                                {isSelf ? (
                                  <span className="inline-flex items-center px-2 py-0.5 bg-yellow-500/10 text-yellow-400 border border-yellow-500/20 text-[10px] font-bold uppercase">
                                    Admin (Self)
                                  </span>
                                ) : (
                                  <select
                                    value={isSuspended ? "suspended" : u.role}
                                    disabled={isSelf}
                                    onChange={(e) => handleRoleChange(u.id, e.target.value)}
                                    className="bg-zinc-900 border border-zinc-800 text-xs px-2 py-1 text-zinc-200 focus:outline-none focus:border-yellow-600 rounded-none font-mono"
                                  >
                                    <option value="viewer">Viewer</option>
                                    <option value="operator">Operator</option>
                                    <option value="admin">Admin</option>
                                    <option value="suspended">Suspended</option>
                                  </select>
                                )}
                              </td>

                              {/* Status */}
                              <td className="py-3 px-3">
                                {isSuspended ? (
                                  <span className="inline-flex items-center gap-1 px-1.5 py-0.5 bg-rose-500/10 text-rose-400 border border-rose-500/20 text-[10px] font-bold uppercase">
                                    <Ban className="h-2.5 w-2.5" />
                                    Suspended
                                  </span>
                                ) : (
                                  <span className="inline-flex items-center gap-1 px-1.5 py-0.5 bg-emerald-500/10 text-emerald-400 border border-emerald-500/20 text-[10px] font-bold uppercase">
                                    <Check className="h-2.5 w-2.5" />
                                    Active
                                  </span>
                                )}
                              </td>

                              {/* Tenant Scope */}
                              <td className="py-3 px-3 text-zinc-400 text-[11px]">
                                {u.allowed_vcenter_ids && u.allowed_vcenter_ids.length > 0 ? (
                                  <span>{u.allowed_vcenter_ids.length} vCenters</span>
                                ) : u.allowed_folder_paths && u.allowed_folder_paths.length > 0 ? (
                                  <span>{u.allowed_folder_paths.join(", ")}</span>
                                ) : (
                                  <span className="text-zinc-500">Global Unrestricted</span>
                                )}
                              </td>

                              {/* Created At */}
                              <td className="py-3 px-3 text-zinc-500 text-[11px]">
                                {new Date(u.created_at).toLocaleDateString()}
                              </td>

                              {/* Actions */}
                              <td className="py-3 px-3 text-right">
                                {!isSelf && (
                                  <div className="flex items-center justify-end gap-1.5">
                                    {/* Suspend / Reactivate button */}
                                    <Button
                                      variant="ghost"
                                      size="sm"
                                      onClick={() => handleToggleSuspend(u)}
                                      title={isSuspended ? "Reactivate user" : "Suspend user"}
                                      className={`h-7 px-2 text-[10px] rounded-none border ${
                                        isSuspended
                                          ? "border-emerald-600/30 text-emerald-400 hover:bg-emerald-500/10"
                                          : "border-zinc-800 text-zinc-400 hover:text-amber-400 hover:border-amber-600/30"
                                      }`}
                                    >
                                      {isSuspended ? "Activate" : "Suspend"}
                                    </Button>

                                    {/* Delete button */}
                                    <Button
                                      variant="ghost"
                                      size="sm"
                                      onClick={() =>
                                        setDeleteTarget({ id: u.id, username: u.username })
                                      }
                                      title="Delete user account"
                                      className="h-7 w-7 p-0 text-zinc-500 hover:text-rose-400 hover:bg-rose-500/10 rounded-none"
                                    >
                                      <Trash2 className="h-3.5 w-3.5" />
                                    </Button>
                                  </div>
                                )}
                              </td>
                            </tr>
                          );
                        })}
                      </tbody>
                    </table>
                  </div>
                )}
              </div>
            </TabsContent>
          )}

          {/* Diagnostics Tab */}
          <TabsContent value="diagnostics" className="space-y-4 m-0">
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
                    {currentUser?.username || "admin"}
                  </div>
                </div>

                <div className="p-3 rounded-none bg-zinc-900/60 border border-zinc-800">
                  <div className="text-[11px] font-mono text-zinc-500 mb-1 uppercase">Role / Privilege</div>
                  <div className="flex items-center gap-2">
                    <span className="capitalize font-semibold text-zinc-200">
                      {currentUser?.role || "admin"}
                    </span>
                    <span className="px-1.5 py-0.2 rounded-none text-[10px] font-mono font-bold uppercase tracking-wider bg-yellow-600/20 text-yellow-500 border border-yellow-600/30">
                      Full Access
                    </span>
                  </div>
                </div>

                <div className="p-3 rounded-none bg-zinc-900/60 border border-zinc-800">
                  <div className="text-[11px] font-mono text-zinc-500 mb-1 uppercase">Email</div>
                  <div className="text-zinc-300 font-mono">
                    {currentUser?.email || "admin@sims.local"}
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

            {/* Security Policies */}
            <div className="border border-zinc-800 bg-[#0a0a0a] p-4 rounded-none space-y-3">
              <h3 className="text-xs font-mono font-bold uppercase tracking-wider text-zinc-200 flex items-center gap-2">
                <Shield className="h-3.5 w-3.5 text-emerald-400" />
                Security & Authentication Guardrails
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
          </TabsContent>
        </Tabs>
      )}

      {/* Add User Modal */}
      <ModalPopup
        isOpen={isAddUserOpen}
        onClose={() => setIsAddUserOpen(false)}
        title="Provision User Account"
        icon={<UserPlus className="h-4 w-4 text-yellow-500" />}
      >
        <form onSubmit={handleCreateUser} className="space-y-4 font-sans" autoComplete="off">
          {addUserError && (
            <div className="p-2.5 bg-rose-500/10 border border-rose-500/20 text-rose-400 text-xs flex items-center gap-2">
              <AlertCircle className="h-4 w-4 shrink-0" />
              <span>{addUserError}</span>
            </div>
          )}

          <div>
            <label className="block text-[11px] font-mono uppercase text-zinc-400 mb-1">
              Username *
            </label>
            <input
              type="text"
              required
              placeholder="Username"
              value={userFormData.username}
              onChange={(e) =>
                setUserFormData({ ...userFormData, username: e.target.value })
              }
              autoComplete="off"
              className="w-full bg-zinc-900 border border-zinc-800 px-3 py-2 text-xs font-mono text-zinc-100 placeholder:text-zinc-600 focus:outline-none focus:border-yellow-600"
            />
          </div>

          <div>
            <label className="block text-[11px] font-mono uppercase text-zinc-400 mb-1">
              Password (min 6 characters) *
            </label>
            <input
              type="password"
              required
              placeholder="Password"
              value={userFormData.password}
              onChange={(e) =>
                setUserFormData({ ...userFormData, password: e.target.value })
              }
              autoComplete="new-password"
              className="w-full bg-zinc-900 border border-zinc-800 px-3 py-2 text-xs font-mono text-zinc-100 placeholder:text-zinc-600 focus:outline-none focus:border-yellow-600"
            />
          </div>

          <div>
            <label className="block text-[11px] font-mono uppercase text-zinc-400 mb-1">
              Email (Optional)
            </label>
            <input
              type="email"
              placeholder="Email address"
              value={userFormData.email}
              onChange={(e) =>
                setUserFormData({ ...userFormData, email: e.target.value })
              }
              autoComplete="off"
              className="w-full bg-zinc-900 border border-zinc-800 px-3 py-2 text-xs font-mono text-zinc-100 placeholder:text-zinc-600 focus:outline-none focus:border-yellow-600"
            />
          </div>

          <div>
            <label className="block text-[11px] font-mono uppercase text-zinc-400 mb-1">
              Assigned Role *
            </label>
            <select
              value={userFormData.role}
              onChange={(e) =>
                setUserFormData({ ...userFormData, role: e.target.value })
              }
              className="w-full bg-zinc-900 border border-zinc-800 px-3 py-2 text-xs font-mono text-zinc-100 focus:outline-none focus:border-yellow-600"
            >
              <option value="viewer">Viewer (Read-Only Access)</option>
              <option value="operator">Operator (Standard Workload Access)</option>
              <option value="admin">Administrator (Full Access & User Management)</option>
            </select>
          </div>

          <div className="flex items-center justify-end gap-2 pt-3 border-t border-zinc-850">
            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={() => setIsAddUserOpen(false)}
              className="h-8 text-xs rounded-none border-zinc-800 text-zinc-400 hover:text-white"
            >
              Cancel
            </Button>
            <Button
              type="submit"
              size="sm"
              disabled={addUserSubmitting}
              className="h-8 text-xs rounded-none bg-yellow-600 hover:bg-yellow-500 text-black font-semibold gap-1.5"
            >
              {addUserSubmitting && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
              {addUserSubmitting ? "Provisioning..." : "Provision User"}
            </Button>
          </div>
        </form>
      </ModalPopup>

      {/* Delete User Confirmation Modal */}
      <ConfirmModal
        isOpen={!!deleteTarget}
        onClose={() => setDeleteTarget(null)}
        onConfirm={handleConfirmDelete}
        title="Delete User Account"
        message={`Are you sure you want to permanently delete user '${deleteTarget?.username}'? This operator will no longer be able to authenticate.`}
        confirmText="Delete Account"
        variant="danger"
        loading={deleteLoading}
      />

      {/* Alert / Notification Modal */}
      {alertInfo && (
        <AlertModal
          isOpen={alertInfo.isOpen}
          onClose={() => setAlertInfo(null)}
          title={alertInfo.title}
          message={alertInfo.message}
          details={alertInfo.details}
          type={alertInfo.type}
        />
      )}
    </div>
  );
}
