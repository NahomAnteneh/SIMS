"use client";

import React, { useState, useEffect } from "react";
import { 
  BarChart3, 
  Download, 
  FileText, 
  Server, 
  Layers, 
  ShieldCheck, 
  Loader2, 
  CheckCircle2,
  Table
} from "lucide-react";
import { simsApi } from "@/lib/api/client";
import { Button } from "@/components/ui/button";

interface ReportSummary {
  totalVMs: number;
  totalHosts: number;
  totalClusters: number;
  totalDatacenters: number;
  tagsCount: number;
  complianceCount: number;
}

export default function ReportsPage() {
  const [loading, setLoading] = useState(true);
  const [summary, setSummary] = useState<ReportSummary>({
    totalVMs: 0,
    totalHosts: 0,
    totalClusters: 0,
    totalDatacenters: 0,
    tagsCount: 0,
    complianceCount: 0,
  });

  const [osData, setOsData] = useState<{ os: string; count: number }[]>([]);
  const [tagData, setTagData] = useState<{ category: string; tag: string; count: number }[]>([]);

  useEffect(() => {
    async function loadReports() {
      setLoading(true);
      try {
        const [facets, hosts, clusters, dcs, tags] = await Promise.all([
          simsApi.getVMFacets("guest_os,power_state").catch(() => null),
          simsApi.listHosts().catch(() => []),
          simsApi.listClusters().catch(() => []),
          simsApi.listDatacenters().catch(() => []),
          simsApi.listTags().catch(() => []),
        ]);

        const rawOS = facets?.facets?.guest_os || [];
        const rawPower = facets?.facets?.power_state || [];
        const totalVMs = rawPower.reduce((acc, c) => acc + c.count, 0) || 1000;

        setSummary({
          totalVMs,
          totalHosts: hosts.length,
          totalClusters: clusters.length,
          totalDatacenters: dcs.length,
          tagsCount: tags.length,
          complianceCount: tags.filter((t) => t.category_name === "Compliance").reduce((acc, c) => acc + c.vm_count, 0),
        });

        setOsData(rawOS.map((o) => ({ os: o.value, count: o.count })));
        setTagData(tags.map((t) => ({ category: t.category_name, tag: t.tag_name, count: t.vm_count })));
      } catch (err) {
        console.error("Failed to generate inventory reports", err);
      } finally {
        setLoading(false);
      }
    }

    loadReports();
  }, []);

  const handleExportJSON = () => {
    const reportObj = {
      generatedAt: new Date().toISOString(),
      summary,
      operatingSystems: osData,
      tagsAndCompliance: tagData,
    };
    const blob = new Blob([JSON.stringify(reportObj, null, 2)], { type: "application/json" });
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = `sims-inventory-report-${new Date().toISOString().slice(0, 10)}.json`;
    a.click();
    URL.revokeObjectURL(url);
  };

  return (
    <div className="space-y-6 max-w-7xl mx-auto p-2 md:p-4">
      {/* Header */}
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4">
        <div>
          <h1 className="text-2xl font-semibold text-zinc-100 flex items-center gap-2.5">
            <BarChart3 className="h-6 w-6 text-yellow-500" />
            Inventory Audit Reports
          </h1>
          <p className="text-sm text-zinc-500 mt-1">
            Standard inventory audit reports, compliance breakdowns, and downloadable telemetry.
          </p>
        </div>

        <Button
          onClick={handleExportJSON}
          disabled={loading}
          className="bg-yellow-600 hover:bg-yellow-500 text-black font-semibold gap-1.5 h-8 text-xs"
        >
          <Download className="h-3.5 w-3.5" />
          Export JSON Report
        </Button>
      </div>

      {/* KPI Overview */}
      <div className="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-6 gap-3">
        <div className="p-3.5 rounded-xl border border-zinc-800 bg-[#0d0d0d]">
          <div className="text-[11px] text-zinc-500">Total Workloads</div>
          <div className="text-xl font-bold text-zinc-100 mt-1">{loading ? "..." : summary.totalVMs}</div>
        </div>
        <div className="p-3.5 rounded-xl border border-zinc-800 bg-[#0d0d0d]">
          <div className="text-[11px] text-zinc-500">ESXi Hosts</div>
          <div className="text-xl font-bold text-zinc-100 mt-1">{loading ? "..." : summary.totalHosts}</div>
        </div>
        <div className="p-3.5 rounded-xl border border-zinc-800 bg-[#0d0d0d]">
          <div className="text-[11px] text-zinc-500">HA Clusters</div>
          <div className="text-xl font-bold text-zinc-100 mt-1">{loading ? "..." : summary.totalClusters}</div>
        </div>
        <div className="p-3.5 rounded-xl border border-zinc-800 bg-[#0d0d0d]">
          <div className="text-[11px] text-zinc-500">Datacenters</div>
          <div className="text-xl font-bold text-zinc-100 mt-1">{loading ? "..." : summary.totalDatacenters}</div>
        </div>
        <div className="p-3.5 rounded-xl border border-zinc-800 bg-[#0d0d0d]">
          <div className="text-[11px] text-zinc-500">Assigned Tags</div>
          <div className="text-xl font-bold text-zinc-100 mt-1">{loading ? "..." : summary.tagsCount}</div>
        </div>
        <div className="p-3.5 rounded-xl border border-zinc-800 bg-[#0d0d0d]">
          <div className="text-[11px] text-zinc-500">Compliance Bound</div>
          <div className="text-xl font-bold text-emerald-400 mt-1">{loading ? "..." : summary.complianceCount}</div>
        </div>
      </div>

      {/* Operating System Distribution Table */}
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
        <div className="bg-[#0a0a0a] border border-zinc-800/80 rounded-xl p-5 shadow-lg space-y-4">
          <h3 className="text-sm font-semibold text-zinc-200 flex items-center gap-2">
            <Server className="h-4 w-4 text-blue-400" />
            Operating System Breakdown
          </h3>

          <div className="border border-zinc-800 rounded-lg overflow-hidden">
            <table className="w-full text-left text-xs">
              <thead className="bg-zinc-900/60 border-b border-zinc-800 text-zinc-400 font-semibold uppercase text-[10px]">
                <tr>
                  <th className="py-2.5 px-3">Guest OS Identifier</th>
                  <th className="py-2.5 px-3">VM Count</th>
                  <th className="py-2.5 px-3">Distribution</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-zinc-900 font-mono">
                {osData.map((row) => (
                  <tr key={row.os} className="hover:bg-zinc-900/30">
                    <td className="py-2.5 px-3 text-zinc-300">{row.os}</td>
                    <td className="py-2.5 px-3 text-zinc-100 font-semibold">{row.count}</td>
                    <td className="py-2.5 px-3 text-zinc-400">
                      {summary.totalVMs > 0 ? Math.round((row.count / summary.totalVMs) * 100) : 0}%
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>

        {/* Governance & Compliance Tags Table */}
        <div className="bg-[#0a0a0a] border border-zinc-800/80 rounded-xl p-5 shadow-lg space-y-4">
          <h3 className="text-sm font-semibold text-zinc-200 flex items-center gap-2">
            <ShieldCheck className="h-4 w-4 text-emerald-400" />
            vSphere Tag & Governance Allocation
          </h3>

          <div className="border border-zinc-800 rounded-lg overflow-hidden max-h-[340px] overflow-y-auto">
            <table className="w-full text-left text-xs">
              <thead className="bg-zinc-900/60 border-b border-zinc-800 text-zinc-400 font-semibold uppercase text-[10px] sticky top-0">
                <tr>
                  <th className="py-2.5 px-3">Category</th>
                  <th className="py-2.5 px-3">Tag Name</th>
                  <th className="py-2.5 px-3">Attached VMs</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-zinc-900 font-mono">
                {tagData.map((row, idx) => (
                  <tr key={idx} className="hover:bg-zinc-900/30">
                    <td className="py-2 px-3 text-zinc-400 font-sans font-medium">{row.category}</td>
                    <td className="py-2 px-3 text-zinc-200">{row.tag}</td>
                    <td className="py-2 px-3 text-emerald-400 font-semibold">{row.count}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      </div>
    </div>
  );
}
