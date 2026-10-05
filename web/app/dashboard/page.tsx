import { MetricCard } from '@/components/ui/MetricCard';
import { 
  Server, 
  Building2, 
  Cpu, 
  Layers, 
  Users, 
  AppWindow 
} from 'lucide-react';

import { InfraService } from "@/lib/services/infra";

export default async function ReportsDashboard() {
  const vms = await InfraService.getVms();
  const datacenters = await InfraService.getDatacenters();
  const vcenters = await InfraService.getVcenters();
  const clusters = await InfraService.getClusters();
  const clients = await InfraService.getClients();
  const applications = await InfraService.getApplications();

  return (
    <div className="max-w-7xl mx-auto space-y-6 p-4 md:p-6">
      {/* Page Header */}
      <div>
        <h1 className="text-2xl font-semibold text-zinc-100">Reports</h1>
        <p className="text-sm text-zinc-500 mt-1">
          Analytics and insights for your inventory
        </p>
      </div>

      {/* Responsive Infrastructure Metrics Grid (4 Columns) */}
      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
        <MetricCard 
          title="Virtual Machines" 
          value={`${vms.length}`}
          description="Total provisioned VMs" 
          icon={<Server className="h-4 w-4" />} 
        />
        <MetricCard 
          title="Datacenters" 
          value={`${datacenters.length}`}
          description="Active physical facilities" 
          icon={<Building2 className="h-4 w-4" />} 
        />
        <MetricCard 
          title="vCenters" 
          value={`${vcenters.length}`}
          description="Management instances" 
          icon={<Cpu className="h-4 w-4" />} 
        />
        <MetricCard 
          title="Clusters" 
          value={`${clusters.length}`}
          description="Compute resource groups" 
          icon={<Layers className="h-4 w-4" />} 
        />
        <MetricCard 
          title="Clients" 
          value={`${clients.length}`}
          description="Registered organizations" 
          icon={<Users className="h-4 w-4" />} 
        />
        <MetricCard 
          title="Applications" 
          value={`${applications.length}`}
          description="Deployed services" 
          icon={<AppWindow className="h-4 w-4" />} 
        />
      </div>

      {/* Main Charts Grid */}
      <div className="grid grid-cols-1 lg:grid-cols-3 gap-4">
        
        {/* Infrastructure Usage Trend - Spans 2 columns */}
        <div className="lg:col-span-2 bg-[#111111] border border-zinc-800/80 rounded-xl p-5 min-h-[300px] flex flex-col">
          <h3 className="text-sm font-medium text-zinc-300 mb-6">Resource Allocation Trend</h3>
          <div className="flex-1 flex items-end relative overflow-hidden pb-6">
            <div className="absolute inset-0 bg-gradient-to-t from-yellow-900/10 to-transparent opacity-50" />
            <svg viewBox="0 0 100 40" preserveAspectRatio="none" className="w-full h-full stroke-yellow-600 fill-none opacity-80" strokeWidth="0.5">
              <path d="M0,35 Q10,34 20,32 T40,28 T60,25 T80,18 T100,10" />
            </svg>
            <div className="absolute bottom-0 w-full flex justify-between text-[10px] text-zinc-600 pt-2">
              <span>May 2025</span><span>Jul 2025</span><span>Sep 2025</span>
              <span>Nov 2025</span><span>Jan 2026</span><span>Mar 2026</span>
            </div>
          </div>
        </div>

        {/* Workload Breakdown - Spans 1 column */}
        <div className="bg-[#111111] border border-zinc-800/80 rounded-xl p-5 min-h-[300px] flex flex-col">
          <h3 className="text-sm font-medium text-zinc-300 mb-6">Infrastructure Distribution</h3>
          <div className="flex-1 flex flex-col items-center justify-center">
            <div className="relative w-48 h-48 rounded-full bg-zinc-800 mb-6"
                 style={{
                   background: 'conic-gradient(#3b82f6 0% 35%, #8b5cf6 35% 60%, #ec4899 60% 80%, #f59e0b 80% 100%)'
                 }}>
              <div className="absolute inset-4 rounded-full bg-[#111111]" />
            </div>
            
            <div className="flex flex-wrap justify-center gap-x-4 gap-y-2 text-[10px] text-zinc-500">
              <span className="flex items-center gap-1"><span className="w-1.5 h-1.5 rounded-full bg-blue-500"></span> VMs</span>
              <span className="flex items-center gap-1"><span className="w-1.5 h-1.5 rounded-full bg-purple-500"></span> Applications</span>
              <span className="flex items-center gap-1"><span className="w-1.5 h-1.5 rounded-full bg-pink-500"></span> Datacenters</span>
              <span className="flex items-center gap-1"><span className="w-1.5 h-1.5 rounded-full bg-yellow-500"></span> Clusters</span>
            </div>
          </div>
        </div>

      </div>

      {/* Demand Forecast Chart */}
      <div className="bg-[#111111] border border-zinc-800/80 rounded-xl p-5 min-h-[300px] flex flex-col">
        <h3 className="text-sm font-medium text-zinc-300 mb-6">Capacity Forecast</h3>
        <div className="flex-1 flex items-end relative overflow-hidden pb-6">
          <svg viewBox="0 0 100 40" preserveAspectRatio="none" className="w-full h-full fill-none opacity-80" strokeWidth="0.5">
            <path d="M0,25 L15,24 L30,26 L45,22 L60,20" className="stroke-yellow-600" />
            <path d="M60,20 L75,18 L90,17 L100,16" className="stroke-blue-500 stroke-dasharray-[1_1]" strokeDasharray="1,1" />
          </svg>
          <div className="absolute bottom-0 w-full flex justify-between text-[10px] text-zinc-600 pt-2">
            <span>Jul</span><span>Aug</span><span>Sep</span><span>Oct</span><span>Nov</span>
            <span>Dec</span><span>Jan</span><span>Feb</span><span>Mar</span><span>Apr</span><span>May</span><span>Jun</span>
          </div>
        </div>
        <div className="flex justify-center gap-4 text-[10px] text-zinc-500 mt-2">
          <span className="flex items-center gap-1"><span className="w-3 h-[2px] bg-yellow-600"></span> Used Capacity</span>
          <span className="flex items-center gap-1"><span className="w-3 h-[2px] border-b border-dashed border-blue-500"></span> Forecasted Capacity</span>
        </div>
      </div>
      
    </div>
  );
}