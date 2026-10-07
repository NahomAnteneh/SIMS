"use client";

import React, { useState, useEffect, useCallback } from "react";
import { Shield, UserPlus, Trash2, Loader2, X, AlertCircle, CheckCircle2, User, Key } from "lucide-react";
import { simsApi } from "@/lib/api/client";
import { UserResponse } from "@/lib/api/types";
import { Button } from "@/components/ui/button";

export default function AccessPage() {
  const [users, setUsers] = useState<UserResponse[]>([]);
  const [loading, setLoading] = useState(true);
  const [currentUser, setCurrentUser] = useState<UserResponse | null>(null);

  // Add User Modal State
  const [isModalOpen, setIsModalOpen] = useState(false);
  const [formData, setFormData] = useState({
    username: "",
    password: "",
    email: "",
    role: "user",
  });
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const loadData = useCallback(async () => {
    setLoading(true);
    try {
      const [me, userList] = await Promise.all([
        simsApi.getMe().catch(() => null),
        simsApi.listUsers().catch(() => []),
      ]);
      setCurrentUser(me);
      setUsers(userList);
    } catch (err) {
      console.error("Failed to load users:", err);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    loadData();
  }, [loadData]);

  const handleCreateUser = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!formData.username || !formData.password) {
      setError("Username and password are required.");
      return;
    }

    setSubmitting(true);
    setError(null);
    try {
      await simsApi.createUser({
        username: formData.username,
        password: formData.password,
        email: formData.email || undefined,
        role: formData.role,
      });

      setIsModalOpen(false);
      setFormData({ username: "", password: "", email: "", role: "user" });
      await loadData();
    } catch (err: any) {
      setError(err?.message || "Failed to create user.");
    } finally {
      setSubmitting(false);
    }
  };

  const handleDeleteUser = async (userId: string, username: string) => {
    if (confirm(`Are you sure you want to delete user '${username}'?`)) {
      try {
        await simsApi.deleteUser(userId);
        await loadData();
      } catch (err: any) {
        alert(err?.message || "Failed to delete user");
      }
    }
  };

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-semibold text-zinc-100">
            Access Control & Governance
          </h1>
          <p className="text-sm text-zinc-500 mt-1">
            Manage local user accounts, RBAC administrative roles, and tenant folder scopes.
          </p>
        </div>

        <Button
          onClick={() => {
            setIsModalOpen(true);
            setError(null);
          }}
          className="bg-yellow-600 hover:bg-yellow-500 text-black font-semibold text-xs gap-1.5 h-9"
        >
          <UserPlus className="h-4 w-4" />
          Add User
        </Button>
      </div>

      {loading ? (
        <div className="h-64 flex flex-col items-center justify-center gap-3 text-zinc-500">
          <Loader2 className="h-6 w-6 animate-spin text-yellow-500" />
          <span className="text-sm">Loading users and permissions...</span>
        </div>
      ) : users.length === 0 ? (
        <div className="p-12 text-center border border-zinc-800 rounded-none bg-zinc-950/20 space-y-3">
          <Shield className="h-10 w-10 text-zinc-600 mx-auto" />
          <h3 className="text-base font-semibold text-zinc-200">No Users Found</h3>
          <p className="text-sm text-zinc-500 max-w-md mx-auto">
            You are currently authenticated as the local administrator.
          </p>
        </div>
      ) : (
        <div className="rounded-none border border-zinc-800 bg-[#0e0e0e] overflow-hidden">
          <table className="w-full text-left text-sm text-zinc-200">
            <thead className="border-b border-zinc-800 bg-zinc-950/60 text-xs font-semibold text-zinc-400">
              <tr>
                <th className="px-5 py-3.5">User</th>
                <th className="px-5 py-3.5">Role</th>
                <th className="px-5 py-3.5">Scope Boundaries</th>
                <th className="px-5 py-3.5">Created At</th>
                <th className="px-5 py-3.5 text-right">Actions</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-zinc-800/60">
              {users.map((u) => {
                const isAdmin = u.role === "admin";
                const isSelf = currentUser?.id === u.id;

                return (
                  <tr key={u.id} className="hover:bg-zinc-900/40 transition-colors">
                    <td className="px-5 py-4">
                      <div className="flex items-center gap-3">
                        <div className="p-2 rounded-none bg-zinc-900 border border-zinc-800 text-zinc-400">
                          <User className="h-4 w-4" />
                        </div>
                        <div>
                          <div className="font-semibold text-zinc-100 flex items-center gap-2">
                            {u.username}
                            {isSelf && (
                              <span className="text-[10px] bg-zinc-800 text-zinc-400 px-1.5 py-0.5 rounded-none font-mono">
                                you
                              </span>
                            )}
                          </div>
                          {u.email && <div className="text-xs text-zinc-500">{u.email}</div>}
                        </div>
                      </div>
                    </td>

                    <td className="px-5 py-4">
                      <span
                        className={`inline-flex items-center px-2 py-0.5 rounded-none text-xs font-mono font-bold capitalize border ${
                          isAdmin
                            ? "bg-amber-500/10 text-amber-400 border-amber-500/20"
                            : "bg-blue-500/10 text-blue-400 border-blue-500/20"
                        }`}
                      >
                        {u.role}
                      </span>
                    </td>

                    <td className="px-5 py-4 text-xs text-zinc-400 font-mono">
                      {u.allowed_vcenter_ids && u.allowed_vcenter_ids.length > 0 ? (
                        <span>{u.allowed_vcenter_ids.length} vCenters</span>
                      ) : u.allowed_folder_paths && u.allowed_folder_paths.length > 0 ? (
                        <span>{u.allowed_folder_paths.join(", ")}</span>
                      ) : (
                        <span className="text-zinc-500">Global / Unrestricted</span>
                      )}
                    </td>

                    <td className="px-5 py-4 text-xs text-zinc-400">
                      {new Date(u.created_at).toLocaleDateString()}
                    </td>

                    <td className="px-5 py-4 text-right">
                      {!isSelf && (
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() => handleDeleteUser(u.id, u.username)}
                          className="h-8 text-zinc-500 hover:text-rose-400 hover:bg-rose-500/10 p-2 rounded-none"
                        >
                          <Trash2 className="h-4 w-4" />
                        </Button>
                      )}
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}

      {/* Add User Modal */}
      {isModalOpen && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/70 backdrop-blur-sm p-4">
          <div className="w-full max-w-md rounded-none bg-[#121212] border border-zinc-800 shadow-2xl p-6 text-zinc-200 font-sans space-y-5">
            <div className="flex items-center justify-between pb-3 border-b border-zinc-800">
              <h3 className="text-base font-bold text-white flex items-center gap-2">
                <UserPlus className="h-4 w-4 text-yellow-500" />
                Add User Account
              </h3>
              <button
                onClick={() => setIsModalOpen(false)}
                className="text-zinc-500 hover:text-zinc-300 p-1 rounded-none"
              >
                <X className="h-5 w-5" />
              </button>
            </div>

            {error && (
              <div className="p-3 rounded-none bg-rose-500/10 border border-rose-500/20 text-rose-400 text-xs flex items-center gap-2">
                <AlertCircle className="h-4 w-4 shrink-0" />
                <span>{error}</span>
              </div>
            )}

            <form onSubmit={handleCreateUser} className="space-y-4">
              <div>
                <label className="block text-xs font-medium text-zinc-400 mb-1">Username</label>
                <input
                  type="text"
                  required
                  placeholder="e.g. operator1"
                  value={formData.username}
                  onChange={(e) => setFormData({ ...formData, username: e.target.value })}
                  className="w-full bg-zinc-900 border border-zinc-800 rounded-none px-3 py-2 text-sm text-zinc-100 placeholder:text-zinc-600 focus:outline-none focus:border-yellow-600"
                />
              </div>

              <div>
                <label className="block text-xs font-medium text-zinc-400 mb-1">Password</label>
                <input
                  type="password"
                  required
                  placeholder="••••••••••••"
                  value={formData.password}
                  onChange={(e) => setFormData({ ...formData, password: e.target.value })}
                  className="w-full bg-zinc-900 border border-zinc-800 rounded-none px-3 py-2 text-sm text-zinc-100 placeholder:text-zinc-600 focus:outline-none focus:border-yellow-600"
                />
              </div>

              <div>
                <label className="block text-xs font-medium text-zinc-400 mb-1">Email (Optional)</label>
                <input
                  type="email"
                  placeholder="operator@company.com"
                  value={formData.email}
                  onChange={(e) => setFormData({ ...formData, email: e.target.value })}
                  className="w-full bg-zinc-900 border border-zinc-800 rounded-none px-3 py-2 text-sm text-zinc-100 placeholder:text-zinc-600 focus:outline-none focus:border-yellow-600"
                />
              </div>

              <div>
                <label className="block text-xs font-medium text-zinc-400 mb-1">Role</label>
                <select
                  value={formData.role}
                  onChange={(e) => setFormData({ ...formData, role: e.target.value })}
                  className="w-full bg-zinc-900 border border-zinc-800 rounded-none px-3 py-2 text-sm text-zinc-100 focus:outline-none focus:border-yellow-600"
                >
                  <option value="user">User (Standard Access)</option>
                  <option value="admin">Administrator (Full Access)</option>
                </select>
              </div>

              <div className="flex items-center justify-end gap-2 pt-3 border-t border-zinc-800">
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  onClick={() => setIsModalOpen(false)}
                  className="text-xs text-zinc-400 hover:text-white"
                >
                  Cancel
                </Button>
                <Button
                  type="submit"
                  size="sm"
                  disabled={submitting}
                  className="bg-yellow-600 hover:bg-yellow-500 text-black font-semibold text-xs gap-1.5"
                >
                  {submitting && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
                  {submitting ? "Creating..." : "Create User"}
                </Button>
              </div>
            </form>
          </div>
        </div>
      )}
    </div>
  );
}
