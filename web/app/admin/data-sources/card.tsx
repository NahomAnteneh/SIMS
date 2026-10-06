import React from "react";
import { Database, Clock, RefreshCw } from "lucide-react";
import {
  Card,
  CardHeader,
  CardTitle,
  CardContent,
  CardFooter,
} from "@/components/ui/card";
import { type DataSource } from "./columns";

interface DataSourceCardProps {
  source: DataSource;
}

export const DataSourceCard: React.FC<DataSourceCardProps> = ({ source }) => {
  const isConnected = source.status === "connected";
  const dateStr = source.lastSync ? new Date(source.lastSync).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }) : "N/A";

  return (
    <Card className="bg-[#0a0a0a] border border-zinc-800/80 text-[#e6e4e0] rounded-2xl p-0 overflow-hidden font-sans shadow-lg">
      <CardHeader className="p-5 pb-4 border-b border-zinc-800/80 flex-row items-center justify-between space-y-0">
        <CardTitle className="text-base font-bold tracking-tight text-white flex items-center gap-2 truncate">
          <Database className="h-4 w-4 text-yellow-500 shrink-0" />
          <span className="truncate">{source.name}</span>
        </CardTitle>
        <span className="text-xs text-zinc-500 font-mono">#{source.id}</span>
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

      <CardFooter className="p-5 pt-3 border-t border-zinc-800/60 flex items-center justify-between text-xs">
        <div className="flex items-center gap-1.5 text-zinc-500">
          <Clock className="h-3 w-3" />
          <span>Synced at {dateStr}</span>
        </div>

        <div
          className={`inline-flex items-center gap-2 px-2.5 py-1 rounded-full text-xs font-medium ${
            isConnected
              ? "text-emerald-400 bg-emerald-400/10"
              : "text-rose-400 bg-rose-400/10"
          }`}
        >
          <span
            className={`w-1.5 h-1.5 rounded-full ${
              isConnected ? "bg-emerald-400" : "bg-rose-400"
            }`}
          />
          <span className="capitalize">{source.status}</span>
        </div>
      </CardFooter>
    </Card>
  );
};
