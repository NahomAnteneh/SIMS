"use client";

import React, { useState, useEffect, useCallback } from "react";
import { LayoutList, LayoutGrid, Plus, Loader2, X, CheckCircle2, AlertCircle, RefreshCw, Server } from "lucide-react";

import { dataSourceColumns, type DataSource } from "./columns";
import { DataTable } from "@/components/ui/data-table";
import { DataSourceCard } from "./card";
import { simsApi } from "@/lib/api/client";
import { Button } from "@/components/ui/button";
import { AlertModal, ConfirmModal } from "@/components/ui/modal-popup";

export default function DataSourcesPage() {
  const [view, setView] = useState<"table" | "card">("table");
  const [dataSources, setDataSources] = useState<DataSource[]>([]);
  const [loading, setLoading] = useState(true);
  const [resyncingId, setResyncingId] = useState<string | null>(null);

  // Modal state
  const [isModalOpen, setIsModalOpen] = useState(false);
  const [editingSource, setEditingSource] = useState<DataSource | null>(null);
  const [deletingSource, setDeletingSource] = useState<{ id: string; name: string } | null>(null);
  const [deletingLoading, setDeletingLoading] = useState(false);

  const [formData, setFormData] = useState({
    name: "",
    host: "",
    port: 443,
    username: "",
    password: "",
    insecure_tls: true,
  });

  const [testStatus, setTestStatus] = useState<{
    testing: boolean;
    success?: boolean;
    message?: string;
  }>({ testing: false });

  const [saveLoading, setSaveLoading] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);
  const [alertInfo, setAlertInfo] = useState<{
    isOpen: boolean;
    title: string;
    message: string;
    details?: string;
  }>({ isOpen: false, title: "", message: "" });

  const handleResync = useCallback(async (id: string) => {
    setResyncingId(id);
    try {
      await simsApi.resyncVCenter(id);
      await loadData();
    } catch (err: any) {
      console.error("Resync failed:", err);
      setAlertInfo({
        isOpen: true,
        title: "Synchronization Error",
        message: "Failed to initiate PropertyCollector resync on the target vCenter instance.",
        details: err?.message || String(err),
      });
    } finally {
      setResyncingId(null);
    }
  }, []);

  const handleOpenEdit = useCallback((ds: DataSource) => {
    setEditingSource(ds);
    const parts = ds.endpoint.split(":");
    setFormData({
      name: ds.name,
      host: ds.host || parts[0] || "",
      port: ds.port || Number(parts[1]) || 443,
      username: "",
      password: "",
      insecure_tls: true,
    });
    setTestStatus({ testing: false });
    setSaveError(null);
    setIsModalOpen(true);
  }, []);

  const handleOpenDelete = useCallback((id: string, name: string) => {
    setDeletingSource({ id, name });
  }, []);

  const handleConfirmDelete = async () => {
    if (!deletingSource) return;
    setDeletingLoading(true);
    try {
      await simsApi.deleteVCenter(deletingSource.id);
      setDeletingSource(null);
      await loadData();
    } catch (err: any) {
      setAlertInfo({
        isOpen: true,
        title: "Decommission Error",
        message: `Failed to decommission vCenter "${deletingSource.name}".`,
        details: err?.message || String(err),
      });
    } finally {
      setDeletingLoading(false);
    }
  };

  const loadData = useCallback(async () => {
    setLoading(true);
    try {
      const vcs = await simsApi.listVCenters();
      const mapped: DataSource[] = vcs.map((vc) => ({
        id: vc.id,
        name: vc.name,
        type: "VMware vCenter",
        endpoint: `${vc.host}:${vc.port}`,
        datacenterId: vc.id.slice(0, 8),
        status: vc.status || "connected",
        lastSync: vc.last_sync_at || vc.updated_at,
        syncDurationSeconds: 1,
        objectsDiscovered: 0,
        version: vc.version?.product_version || vc.version?.version || "8.0",
        host: vc.host,
        port: vc.port,
        onResync: handleResync,
        onEdit: handleOpenEdit,
        onDelete: handleOpenDelete,
        isResyncing: resyncingId === vc.id,
      }));
      setDataSources(mapped);
    } catch (err) {
      console.error("Failed to load data sources", err);
    } finally {
      setLoading(false);
    }
  }, [handleResync, handleOpenEdit, handleOpenDelete, resyncingId]);

  useEffect(() => {
    loadData();
  }, [loadData]);

  const handleTestConnection = async () => {
    if (!formData.host || !formData.username || !formData.password) {
      setTestStatus({
        testing: false,
        success: false,
        message: "Please provide host, username, and password",
      });
      return;
    }

    setTestStatus({ testing: true });
    try {
      const res = await simsApi.testVCenterConnection({
        host: formData.host,
        port: Number(formData.port),
        username: formData.username,
        password: formData.password,
        insecure_tls: formData.insecure_tls,
      });

      setTestStatus({
        testing: false,
        success: true,
        message: `Validated! vSphere ${res.version?.version || "Connected"}`,
      });
    } catch (err: any) {
      setTestStatus({
        testing: false,
        success: false,
        message: err?.message || "Connection failed. Please check host, port, and credentials.",
      });
    }
  };

  const handleSave = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!formData.name || !formData.host) {
      setSaveError("Instance name and host are required.");
      return;
    }

    if (!editingSource && (!formData.username || !formData.password)) {
      setSaveError("Username and password are required for initial registration.");
      return;
    }

    setSaveLoading(true);
    setSaveError(null);
    try {
      if (editingSource) {
        await simsApi.updateVCenter(editingSource.id, {
          name: formData.name,
          username: formData.username ? formData.username : undefined,
          password: formData.password ? formData.password : undefined,
        });
        try {
          await simsApi.resyncVCenter(editingSource.id);
        } catch {
          // ignore resync error
        }
      } else {
        await simsApi.createVCenter({
          name: formData.name,
          host: formData.host,
          port: Number(formData.port),
          username: formData.username,
          password: formData.password,
          insecure_tls: formData.insecure_tls,
        });
      }

      setIsModalOpen(false);
      setEditingSource(null);
      setFormData({
        name: "",
        host: "",
        port: 443,
        username: "",
        password: "",
        insecure_tls: true,
      });
      setTestStatus({ testing: false });
      await loadData();
    } catch (err: any) {
      setSaveError(err?.message || "Failed to save vCenter settings.");
    } finally {
      setSaveLoading(false);
    }
  };

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-semibold text-zinc-100">
            Data Sources
          </h1>
          <p className="text-sm text-zinc-500 mt-1">
            Configure integration endpoints, vCenter management planes, and real-time inventory syncs.
          </p>
        </div>

        <div className="flex items-center gap-3">
          <Button
            onClick={() => {
              setEditingSource(null);
              setFormData({
                name: "",
                host: "",
                port: 443,
                username: "",
                password: "",
                insecure_tls: true,
              });
              setIsModalOpen(true);
              setSaveError(null);
              setTestStatus({ testing: false });
            }}
            className="bg-yellow-600 hover:bg-yellow-500 text-black font-semibold text-xs gap-1.5 h-9"
          >
            <Plus className="h-4 w-4" />
            Register vCenter
          </Button>

          {/* View Toggle */}
          <div className="flex items-center rounded-none border border-zinc-800 bg-zinc-900/50 p-1">
            <button
              type="button"
              onClick={() => setView("table")}
              className={`flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium rounded-none transition-colors ${
                view === "table"
                  ? "bg-zinc-800 text-zinc-100 shadow-sm"
                  : "text-zinc-400 hover:text-zinc-200"
              }`}
            >
              <LayoutList className="h-3.5 w-3.5" />
              Table
            </button>
            <button
              type="button"
              onClick={() => setView("card")}
              className={`flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium rounded-none transition-colors ${
                view === "card"
                  ? "bg-zinc-800 text-zinc-100 shadow-sm"
                  : "text-zinc-400 hover:text-zinc-200"
              }`}
            >
              <LayoutGrid className="h-3.5 w-3.5" />
              Card
            </button>
          </div>
        </div>
      </div>

      {loading ? (
        <div className="h-64 flex flex-col items-center justify-center gap-3 text-zinc-500">
          <Loader2 className="h-6 w-6 animate-spin text-yellow-500" />
          <span className="text-sm">Loading registered vCenters...</span>
        </div>
      ) : dataSources.length === 0 ? (
        <div className="p-12 text-center border border-zinc-800 rounded-none bg-zinc-950/20 space-y-3">
          <Server className="h-10 w-10 text-zinc-600 mx-auto" />
          <h3 className="text-base font-semibold text-zinc-200">No vCenter Instances Registered</h3>
          <p className="text-sm text-zinc-500 max-w-md mx-auto">
            Connect a vCenter Server management endpoint to begin continuous streaming ingestion.
          </p>
          <Button
            onClick={() => setIsModalOpen(true)}
            className="mt-2 bg-yellow-600 hover:bg-yellow-500 text-black font-semibold text-xs rounded-none"
          >
            Register vCenter
          </Button>
        </div>
      ) : view === "table" ? (
        <DataTable
          columns={dataSourceColumns}
          data={dataSources}
          searchColumn="name"
          searchPlaceholder="Filter data sources by name..."
        />
      ) : (
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-5">
          {dataSources.map((ds) => (
            <DataSourceCard key={ds.id} source={ds} />
          ))}
        </div>
      )}

      {/* Register vCenter Modal */}
      {isModalOpen && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/80 p-4">
          <div className="w-full max-w-lg rounded-none bg-[#121212] border border-zinc-800 shadow-2xl p-6 text-zinc-200 font-sans space-y-5">
            <div className="flex items-center justify-between pb-3 border-b border-zinc-800">
              <div>
                <h3 className="text-base font-bold text-white">
                  {editingSource ? `Update vCenter: ${editingSource.name}` : "Register vCenter Instance"}
                </h3>
                <p className="text-xs text-zinc-500 mt-0.5">
                  {editingSource
                    ? "Update endpoint configuration and credentials for this vCenter."
                    : "Connect a vSphere endpoint for property collection and change ingestion."}
                </p>
              </div>
              <button
                onClick={() => {
                  setIsModalOpen(false);
                  setEditingSource(null);
                }}
                className="text-zinc-500 hover:text-zinc-300 p-1 rounded-none"
              >
                <X className="h-5 w-5" />
              </button>
            </div>

            {saveError && (
              <div className="p-3 rounded-none bg-rose-500/10 border border-rose-500/20 text-rose-400 text-xs flex items-center gap-2">
                <AlertCircle className="h-4 w-4 shrink-0" />
                <span>{saveError}</span>
              </div>
            )}

            <form onSubmit={handleSave} className="space-y-4" autoComplete="off">
              <div>
                <label className="block text-xs font-medium text-zinc-400 mb-1">Instance Name</label>
                <input
                  type="text"
                  required
                  placeholder="e.g. vc-prod-01"
                  value={formData.name}
                  onChange={(e) => setFormData({ ...formData, name: e.target.value })}
                  autoComplete="off"
                  className="w-full bg-zinc-900 border border-zinc-800 rounded-none px-3 py-2 text-sm text-zinc-100 placeholder:text-zinc-600 focus:outline-none focus:border-yellow-600"
                />
              </div>

              <div className="grid grid-cols-3 gap-3">
                <div className="col-span-2">
                  <label className="block text-xs font-medium text-zinc-400 mb-1">Host / IP Address</label>
                  <input
                    type="text"
                    required
                    placeholder="e.g. 10.0.0.10 or vcenter.corp.internal"
                    value={formData.host}
                    onChange={(e) => setFormData({ ...formData, host: e.target.value })}
                    autoComplete="off"
                    className="w-full bg-zinc-900 border border-zinc-800 rounded-none px-3 py-2 text-sm text-zinc-100 placeholder:text-zinc-600 focus:outline-none focus:border-yellow-600 font-mono"
                  />
                </div>
                <div>
                  <label className="block text-xs font-medium text-zinc-400 mb-1">Port</label>
                  <input
                    type="number"
                    required
                    value={formData.port}
                    onChange={(e) => setFormData({ ...formData, port: Number(e.target.value) })}
                    className="w-full bg-zinc-900 border border-zinc-800 rounded-none px-3 py-2 text-sm text-zinc-100 focus:outline-none focus:border-yellow-600 font-mono"
                  />
                </div>
              </div>

              <div className="grid grid-cols-2 gap-3">
                <div>
                  <label className="block text-xs font-medium text-zinc-400 mb-1">
                    {editingSource ? "Username (leave blank to keep current)" : "Username"}
                  </label>
                  <input
                    type="text"
                    required={!editingSource}
                    placeholder="Username (e.g. svc-sims@vsphere.local)"
                    value={formData.username}
                    onChange={(e) => setFormData({ ...formData, username: e.target.value })}
                    autoComplete="off"
                    className="w-full bg-zinc-900 border border-zinc-800 rounded-none px-3 py-2 text-sm text-zinc-100 placeholder:text-zinc-600 focus:outline-none focus:border-yellow-600"
                  />
                </div>
                <div>
                  <label className="block text-xs font-medium text-zinc-400 mb-1">
                    {editingSource ? "Password (leave blank to keep current)" : "Password"}
                  </label>
                  <input
                    type="password"
                    required={!editingSource}
                    placeholder="Password"
                    value={formData.password}
                    onChange={(e) => setFormData({ ...formData, password: e.target.value })}
                    autoComplete="new-password"
                    className="w-full bg-zinc-900 border border-zinc-800 rounded-none px-3 py-2 text-sm text-zinc-100 placeholder:text-zinc-600 focus:outline-none focus:border-yellow-600"
                  />
                </div>
              </div>

              <div className="flex items-center gap-2 pt-1">
                <input
                  type="checkbox"
                  id="insecure_tls"
                  checked={formData.insecure_tls}
                  onChange={(e) => setFormData({ ...formData, insecure_tls: e.target.checked })}
                  className="rounded border-zinc-800 bg-zinc-900 text-yellow-600 focus:ring-0"
                />
                <label htmlFor="insecure_tls" className="text-xs text-zinc-400 cursor-pointer">
                  Skip TLS certificate verification (Self-signed certificates)
                </label>
              </div>

              {/* Test Connection Banner */}
              {testStatus.message && (
                <div
                  className={`p-3 rounded-none text-xs flex items-center gap-2 border ${
                    testStatus.success
                      ? "bg-emerald-500/10 border-emerald-500/20 text-emerald-400"
                      : "bg-rose-500/10 border-rose-500/20 text-rose-400"
                  }`}
                >
                  {testStatus.success ? (
                    <CheckCircle2 className="h-4 w-4 shrink-0" />
                  ) : (
                    <AlertCircle className="h-4 w-4 shrink-0" />
                  )}
                  <span>{testStatus.message}</span>
                </div>
              )}

              {/* Action Buttons */}
              <div className="flex items-center justify-between pt-3 border-t border-zinc-800">
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  disabled={testStatus.testing || !formData.host || !formData.username || !formData.password}
                  onClick={handleTestConnection}
                  className="text-xs border-zinc-800 hover:bg-zinc-800 gap-1.5"
                >
                  {testStatus.testing ? (
                    <Loader2 className="h-3.5 w-3.5 animate-spin" />
                  ) : (
                    <RefreshCw className="h-3.5 w-3.5" />
                  )}
                  {testStatus.testing ? "Validating..." : "Test Connection"}
                </Button>

                <div className="flex items-center gap-2">
                  <Button
                    type="button"
                    variant="ghost"
                    size="sm"
                    onClick={() => {
                      setIsModalOpen(false);
                      setEditingSource(null);
                    }}
                    className="text-xs text-zinc-400 hover:text-white"
                  >
                    Cancel
                  </Button>
                  <Button
                    type="submit"
                    size="sm"
                    disabled={saveLoading}
                    className="bg-yellow-600 hover:bg-yellow-500 text-black font-semibold text-xs gap-1.5"
                  >
                    {saveLoading && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
                    {saveLoading
                      ? (editingSource ? "Updating..." : "Registering...")
                      : (editingSource ? "Update & Resync" : "Save & Register")}
                  </Button>
                </div>
              </div>
            </form>
          </div>
        </div>
      )}

      {/* Confirm Decommission Modal */}
      <ConfirmModal
        isOpen={Boolean(deletingSource)}
        onClose={() => setDeletingSource(null)}
        onConfirm={handleConfirmDelete}
        title="Decommission vCenter Data Source"
        message={`Are you sure you want to remove "${deletingSource?.name}"? All associated properties and streaming collection pipelines will be stopped.`}
        confirmText="Decommission"
        cancelText="Cancel"
        variant="danger"
        loading={deletingLoading}
      />

      {/* Error / Alert Modal Popup */}
      <AlertModal
        isOpen={alertInfo.isOpen}
        onClose={() => setAlertInfo({ ...alertInfo, isOpen: false })}
        title={alertInfo.title}
        message={alertInfo.message}
        details={alertInfo.details}
        type="error"
      />
    </div>
  );
}
