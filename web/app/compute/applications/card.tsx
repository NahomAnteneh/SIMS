import React from "react";
import { Component, ShieldAlert, Building2 } from "lucide-react";
import {
  Card,
  CardHeader,
  CardTitle,
  CardContent,
  CardFooter,
} from "@/components/ui/card";
import { type Application } from "./columns";

interface ApplicationCardProps {
  app: Application;
}

export const ApplicationCard: React.FC<ApplicationCardProps> = ({ app }) => {
  const isCritical = app.criticality === "critical";

  return (
    <Card className="bg-[#0a0a0a] border border-zinc-800/80 text-[#e6e4e0] rounded-2xl p-0 overflow-hidden font-sans shadow-lg">
      <CardHeader className="p-5 pb-4 border-b border-zinc-800/80 flex-row items-center justify-between space-y-0">
        <CardTitle className="text-base font-bold tracking-tight text-white flex items-center gap-2 truncate">
          <Component className="h-4 w-4 text-indigo-400 shrink-0" />
          <span className="truncate">{app.name}</span>
        </CardTitle>
        <span className="text-xs text-zinc-500 font-mono">#{app.id}</span>
      </CardHeader>

      <CardContent className="p-5 space-y-3.5 text-sm">
        <div className="flex items-center justify-between">
          <span className="text-zinc-500 font-normal">Client / Tenant</span>
          <span className="font-semibold text-zinc-200">{app.clientId}</span>
        </div>

        <div className="flex items-center justify-between">
          <span className="text-zinc-500 font-normal">Environment</span>
          <span className="uppercase text-xs font-semibold px-2 py-0.5 rounded bg-zinc-850 border border-zinc-800 text-zinc-300">
            {app.environment}
          </span>
        </div>

        <div className="flex items-center justify-between">
          <span className="text-zinc-500 font-normal">Criticality</span>
          <span
            className={`capitalize font-semibold text-xs flex items-center gap-1 ${
              isCritical ? "text-rose-400" : "text-amber-400"
            }`}
          >
            <ShieldAlert className="h-3.5 w-3.5" />
            {app.criticality}
          </span>
        </div>

        <div className="flex items-center justify-between">
          <span className="text-zinc-500 font-normal">Footprint</span>
          <span className="font-semibold text-zinc-200">
            {app.vmCount} VMs across {app.datacenterCount} DCs
          </span>
        </div>
      </CardContent>

      <CardFooter className="p-5 pt-3 border-t border-zinc-800/60 flex items-center justify-between text-xs">
        <span className="text-zinc-500">Owner: {app.owner}</span>
        <div className="inline-flex items-center gap-2 px-2.5 py-1 rounded-full text-emerald-400 text-xs font-medium bg-emerald-400/10">
          <span className="w-1.5 h-1.5 rounded-full bg-emerald-400" />
          <span className="capitalize">{app.status}</span>
        </div>
      </CardFooter>
    </Card>
  );
};
