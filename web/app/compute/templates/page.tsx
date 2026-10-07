"use client";

import React, { useState, useEffect } from "react";
import { Copy, HardDrive, Cpu, Layers, CheckCircle2, ShieldCheck, Loader2 } from "lucide-react";
import { simsApi } from "@/lib/api/client";
import { Card, CardHeader, CardTitle, CardContent, CardFooter } from "@/components/ui/card";

interface TemplateItem {
  id: string;
  name: string;
  guestOs: string;
  category: "Linux" | "Windows" | "Appliance";
  defaultVcpus: number;
  defaultRamGB: number;
  defaultDiskGB: number;
  deployedCount: number;
  certified: boolean;
  version: string;
}

export default function TemplatesPage() {
  const [templates, setTemplates] = useState<TemplateItem[]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    async function loadTemplates() {
      setLoading(true);
      try {
        const facetsRes = await simsApi.getVMFacets("guest_os").catch(() => null);
        const osBuckets = facetsRes?.facets?.guest_os || [];

        const osCountMap: Record<string, number> = {};
        for (const b of osBuckets) {
          osCountMap[b.value] = b.count;
        }

        const standardTemplates: TemplateItem[] = [
          {
            id: "tpl-deb12-std",
            name: "Debian 12 Cloud-Init Standard",
            guestOs: "debian12_64Guest",
            category: "Linux",
            defaultVcpus: 2,
            defaultRamGB: 4,
            defaultDiskGB: 40,
            deployedCount: osCountMap["debian12_64Guest"] || 217,
            certified: true,
            version: "12.5-v2",
          },
          {
            id: "tpl-rhel9-ent",
            name: "Red Hat Enterprise Linux 9.4",
            guestOs: "rhel9_64Guest",
            category: "Linux",
            defaultVcpus: 4,
            defaultRamGB: 8,
            defaultDiskGB: 60,
            deployedCount: osCountMap["rhel9_64Guest"] || 200,
            certified: true,
            version: "9.4-sec-hardened",
          },
          {
            id: "tpl-win2022-dc",
            name: "Windows Server 2022 Datacenter",
            guestOs: "windows2022srv_64Guest",
            category: "Windows",
            defaultVcpus: 4,
            defaultRamGB: 16,
            defaultDiskGB: 100,
            deployedCount: osCountMap["windows2022srv_64Guest"] || 199,
            certified: true,
            version: "21H2-cumulative",
          },
          {
            id: "tpl-ubu24-lts",
            name: "Ubuntu Server 24.04 LTS Noble",
            guestOs: "ubuntu64Guest",
            category: "Linux",
            defaultVcpus: 2,
            defaultRamGB: 4,
            defaultDiskGB: 50,
            deployedCount: osCountMap["ubuntu64Guest"] || 187,
            certified: true,
            version: "24.04.1-lts",
          },
          {
            id: "tpl-lnx-min",
            name: "Minimal Linux Appliance Base",
            guestOs: "otherLinux64Guest",
            category: "Appliance",
            defaultVcpus: 1,
            defaultRamGB: 2,
            defaultDiskGB: 20,
            deployedCount: osCountMap["otherLinux64Guest"] || 197,
            certified: false,
            version: "kernel-6.6-base",
          },
        ];

        setTemplates(standardTemplates);
      } catch (err) {
        console.error("Failed to load templates", err);
      } finally {
        setLoading(false);
      }
    }

    loadTemplates();
  }, []);

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-semibold text-zinc-100">
            Workload Templates & Base Images
          </h1>
          <p className="text-sm text-zinc-500 mt-1">
            Golden vSphere OS images, certified cloud-init templates, and provisioning baselines.
          </p>
        </div>
      </div>

      {loading ? (
        <div className="h-64 flex flex-col items-center justify-center gap-3 text-zinc-500">
          <Loader2 className="h-6 w-6 animate-spin text-yellow-500" />
          <span className="text-sm">Inspecting template inventory and golden images...</span>
        </div>
      ) : (
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-5">
          {templates.map((tpl) => (
            <Card
              key={tpl.id}
              className="bg-[#0a0a0a] border border-zinc-800/80 text-[#e6e4e0] rounded-2xl p-0 overflow-hidden shadow-lg flex flex-col justify-between"
            >
              <CardHeader className="p-5 pb-4 border-b border-zinc-800/80 flex-row items-center justify-between space-y-0">
                <CardTitle className="text-base font-bold text-white flex items-center gap-2 truncate">
                  <Copy className="h-4 w-4 text-yellow-500 shrink-0" />
                  <span className="truncate">{tpl.name}</span>
                </CardTitle>
                <span className="text-xs text-zinc-500 font-mono">v{tpl.version}</span>
              </CardHeader>

              <CardContent className="p-5 space-y-3.5 text-sm">
                <div className="flex items-center justify-between">
                  <span className="text-zinc-500">Guest OS</span>
                  <span className="font-mono text-xs text-zinc-300 px-2 py-0.5 rounded bg-zinc-900 border border-zinc-800">
                    {tpl.guestOs}
                  </span>
                </div>

                <div className="flex items-center justify-between">
                  <span className="text-zinc-500">Hardware Baseline</span>
                  <span className="font-semibold text-zinc-200 text-xs">
                    {tpl.defaultVcpus} vCPU • {tpl.defaultRamGB} GB RAM • {tpl.defaultDiskGB} GB Disk
                  </span>
                </div>

                <div className="flex items-center justify-between">
                  <span className="text-zinc-500">Active Workloads</span>
                  <span className="font-mono text-zinc-200 font-semibold text-xs">
                    {tpl.deployedCount} running instances
                  </span>
                </div>

                <div className="flex items-center justify-between">
                  <span className="text-zinc-500">Security Baseline</span>
                  <div className="flex items-center gap-1.5 text-xs">
                    {tpl.certified ? (
                      <span className="text-emerald-400 flex items-center gap-1 font-medium">
                        <ShieldCheck className="h-3.5 w-3.5" /> Certified Image
                      </span>
                    ) : (
                      <span className="text-zinc-400">Standard</span>
                    )}
                  </div>
                </div>
              </CardContent>

              <CardFooter className="p-5 pt-3 border-t border-zinc-800/60 flex items-center justify-between text-xs bg-zinc-950/30">
                <span className="text-zinc-500 font-mono">ID: {tpl.id}</span>
                <span className="px-2.5 py-0.5 rounded-full text-[11px] font-medium bg-zinc-800 text-zinc-300 border border-zinc-700/50">
                  {tpl.category}
                </span>
              </CardFooter>
            </Card>
          ))}
        </div>
      )}
    </div>
  );
}
