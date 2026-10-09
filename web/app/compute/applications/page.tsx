"use client";

import React, { useState, useEffect } from "react";
import { LayoutList, LayoutGrid, Loader2 } from "lucide-react";

import { appColumns, type Application } from "./columns";
import { DataTable } from "@/components/ui/data-table";
import { ApplicationCard } from "./card";
import { simsApi } from "@/lib/api/client";

export default function ApplicationsPage() {
  const [view, setView] = useState<"table" | "card">("table");
  const [applications, setApplications] = useState<Application[]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    async function loadApplications() {
      setLoading(true);
      try {
        const [tags, attrs] = await Promise.all([
          simsApi.listTags().catch(() => []),
          simsApi.listCustomAttributes().catch(() => []),
        ]);

        const tierTags = tags.filter((t) => t.category_name.toLowerCase() === "tier");
        const ownerTags = tags.filter((t) => t.category_name.toLowerCase() === "owner");
        const envTags = tags.filter((t) => t.category_name.toLowerCase() === "environment");

        const defaultOwners = ["platform-team", "sre-team", "data-eng", "security-ops"];
        const mappedApps: Application[] = [];

        // 1. Map Tier tags as Tier services
        tierTags.forEach((t, idx) => {
          const owner = ownerTags[idx % Math.max(1, ownerTags.length)]?.tag_name || defaultOwners[idx % defaultOwners.length];
          const env = envTags[idx % Math.max(1, envTags.length)]?.tag_name || "production";
          const crit = t.tag_name === "database" || t.tag_name === "backend" ? "critical" : t.tag_name === "frontend" ? "high" : "medium";

          mappedApps.push({
            id: `tier-${t.tag_name}`,
            name: `${t.tag_name.charAt(0).toUpperCase() + t.tag_name.slice(1)} Service Tier`,
            clientId: "Enterprise Infrastructure",
            environment: env,
            criticality: crit,
            status: "healthy",
            vmCount: t.vm_count,
            datacenterCount: 2,
            owner: owner,
          });
        });

        // 2. Map ApplicationID custom attribute workloads
        const appAttr = attrs.find((a) => a.name === "ApplicationID");
        if (appAttr && appAttr.sample_values) {
          appAttr.sample_values.slice(0, 6).forEach((appId, idx) => {
            mappedApps.push({
              id: appId.toLowerCase(),
              name: `Application ${appId}`,
              clientId: "Corp Internal Services",
              environment: idx % 2 === 0 ? "production" : "staging",
              criticality: idx === 0 ? "critical" : "high",
              status: "healthy",
              vmCount: Math.round(appAttr.vm_count / Math.max(1, appAttr.sample_values?.length || 1)),
              datacenterCount: 2,
              owner: defaultOwners[idx % defaultOwners.length],
            });
          });
        }

        setApplications(mappedApps);
      } catch (err) {
        console.error("Failed to load application workloads", err);
      } finally {
        setLoading(false);
      }
    }

    loadApplications();
  }, []);

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-semibold text-zinc-100">
            Applications & Workload Services
          </h1>
          <p className="text-sm text-zinc-500 mt-1">
            Workloads organized by vSphere Tier tags, custom attributes, and ownership.
          </p>
        </div>

        {/* View Toggle */}
        <div className="flex items-center rounded-lg border border-zinc-800 bg-zinc-900/50 p-1">
          <button
            type="button"
            onClick={() => setView("table")}
            className={`flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium rounded-md transition-colors ${
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
            className={`flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium rounded-md transition-colors ${
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

      {loading ? (
        <div className="h-64 flex flex-col items-center justify-center gap-3 text-zinc-500">
          <Loader2 className="h-6 w-6 animate-spin text-yellow-500" />
          <span className="text-sm">Mapping workload services from vSphere tags...</span>
        </div>
      ) : view === "table" ? (
        <DataTable
          columns={appColumns}
          data={applications}
          searchColumn="name"
          searchPlaceholder="Filter applications by name..."
        />
      ) : (
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-5">
          {applications.map((app) => (
            <ApplicationCard key={app.id} app={app} />
          ))}
        </div>
      )}
    </div>
  );
}
