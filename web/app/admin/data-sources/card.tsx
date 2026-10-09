import React from "react";
import { Database, Clock, RefreshCw, KeyRound, Trash2 } from "lucide-react";
import {
  Card,
  CardHeader,
  CardTitle,
  CardContent,
  CardFooter,
} from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { type DataSource } from "./columns";

interface DataSourceCardProps {
  source: DataSource;
}

export const DataSourceCard: React.FC<DataSourceCardProps> = ({ source }) => {
  const status = source.status?.toUpperCase() || "";
  const isConnected = status === "CONNECTED" || status === "RUNNING";
  const isSyncing = status === "SYNCING" || status === "ASSIGNED";
  const isDegraded = status === "DEGRADED" || status === "DISCONNECTED";

  let statusBadgeClass = "text-zinc-400 bg-zinc-800 border-zinc-700";
  let statusDotClass = "bg-zinc-400";
  let displayStatus = source.status;

  if (isConnected) {
    statusBadgeClass = "text-emerald-400 bg-emerald-400/10 border-emerald-500/30";
    statusDotClass = "bg-emerald-400";
    displayStatus = "Healthy";
  } else if (isSyncing) {
    statusBadgeClass = "text-yellow-400 bg-yellow-400/10 border-yellow-500/30";
    statusDotClass = "bg-yellow-400";
    displayStatus = "Syncing";
  } else if (isDegraded) {
    statusBadgeClass = "text-rose-400 bg-rose-400/10 border-rose-500/30";
    statusDotClass = "bg-rose-400";
    displayStatus = "Degraded";
  }

  const dateStr = source.lastSync ? new Date(source.lastSync).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }) : "N/A";

  return (
    <Card className="bg-[#0a0a0a] border border-zinc-800 text-[#e6e4e0] rounded-none p-0 overflow-hidden font-sans shadow-lg flex flex-col justify-between">
      <div>
        <CardHeader className="p-5 pb-4 border-b border-zinc-800/80 flex-row items-center justify-between space-y-0">
          <CardTitle className="text-base font-bold tracking-tight text-white flex items-center gap-2 truncate">
            <Database className="h-4 w-4 text-yellow-500 shrink-0" />
            <span className="truncate">{source.name}</span>
          </CardTitle>
          <span className="text-xs text-zinc-500 font-mono">#{source.id.slice(0, 8)}</span>
        </CardHeader>

        <CardContent className="p-5 space-y-3.5 text-sm">
          <div className="flex items-center justify-between">
            <span className="text-zinc-500 font-normal">Provider Type</span>
            <span className="font-semibold text-zinc-200">{source.type}</span>
          </div>

          <div className="flex items-center justify-between">
            <span className="text-zinc-500 font-normal">Endpoint</span>
            <span className="font-mono text-xs text-zinc-300 truncate max-w-[200px]">
              {source.endpoint}
            </span>
          </div>

          <div className="flex items-center justify-between">
            <span className="text-zinc-500 font-normal">Datacenter</span>
            <span className="font-semibold text-zinc-200">{source.datacenterId}</span>
          </div>

          <div className="flex items-center justify-between">
            <span className="text-zinc-500 font-normal">Objects Discovered</span>
            <span className="font-semibold text-zinc-200">
              {source.objectsDiscovered} ({source.syncDurationSeconds}s)
            </span>
          </div>
        </CardContent>
      </div>

      <CardFooter className="p-4 pt-3 border-t border-zinc-800/60 flex flex-col gap-3">
        <div className="flex items-center justify-between w-full text-xs">
          <div className="flex items-center gap-1.5 text-zinc-500 font-mono">
            <Clock className="h-3 w-3" />
            <span>Synced at {dateStr}</span>
          </div>

          <div className={`inline-flex items-center gap-2 px-2 py-0.5 rounded-none text-xs font-mono font-medium border ${statusBadgeClass}`}>
            <span className={`w-1.5 h-1.5 rounded-none ${statusDotClass}`} />
            <span className="capitalize">{displayStatus}</span>
          </div>
        </div>

        <div className="flex items-center gap-2 w-full pt-1 border-t border-zinc-900">
          <Button
            variant="outline"
            size="sm"
            disabled={source.isResyncing}
            onClick={() => source.onResync?.(source.id)}
            className="flex-1 text-xs h-7.5 gap-1.5 border-zinc-800 hover:bg-zinc-800 text-zinc-300 hover:text-white"
          >
            <RefreshCw className={`h-3 w-3 ${source.isResyncing ? "animate-spin text-yellow-500" : ""}`} />
            {source.isResyncing ? "Syncing..." : "Resync"}
          </Button>
          <Button
            variant="outline"
            size="sm"
            onClick={() => source.onEdit?.(source)}
            className="text-xs h-7.5 px-2.5 border-zinc-800 hover:bg-zinc-800 text-zinc-300 hover:text-yellow-400"
            title="Edit credentials or settings"
          >
            <KeyRound className="h-3 w-3" />
          </Button>
          <Button
            variant="outline"
            size="sm"
            onClick={() => source.onDelete?.(source.id, source.name)}
            className="text-xs h-7.5 px-2.5 border-zinc-800 hover:bg-rose-950/40 text-zinc-400 hover:text-rose-400 hover:border-rose-800/50"
            title="Decommission vCenter data source"
          >
            <Trash2 className="h-3 w-3" />
          </Button>
        </div>
      </CardFooter>
    </Card>
  );
};
