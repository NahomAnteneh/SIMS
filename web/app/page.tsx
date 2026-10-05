import { MetricCard } from '@/components/ui/MetricCard';
import { Package, DollarSign, AlertTriangle, TrendingDown } from 'lucide-react';

export default function ReportsDashboard() {
  return (
    <div className="max-w-7xl mx-auto space-y-6">
      {/* Page Header */}
      <div>
        <h1 className="text-2xl font-semibold text-zinc-100">Reports</h1>
        <p className="text-sm text-zinc-500 mt-1">Analytics and insights for your inventory</p>
      </div>

      {/* Metrics Grid */}
      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4">
        <MetricCard 
          title="Total SKUs" 
          value="25" 
          description="Active products" 
          icon={<Package className="h-4 w-4" />} 
        />
        <MetricCard 
          title="Stock Value" 
          value="$91,683" 
          description="Total inventory value" 
          icon={<DollarSign className="h-4 w-4" />} 
        />
        <MetricCard 
          title="Low Stock" 
          value="3" 
          description="Need attention" 
          icon={<AlertTriangle className="h-4 w-4" />} 
        />
        <MetricCard 
          title="Out of Stock" 
          value="1" 
          description="Currently unavailable" 
          icon={<TrendingDown className="h-4 w-4" />} 
        />
      </div>

      {/* Main Charts Grid */}
      <div className="grid grid-cols-1 lg:grid-cols-3 gap-4">
        
        {/* Stock Value Trend - Spans 2 columns */}
        <div className="lg:col-span-2 bg-[#111111] border border-zinc-800/80 rounded-xl p-5 min-h-[300px] flex flex-col">
          <h3 className="text-sm font-medium text-zinc-300 mb-6">Stock Value Trend</h3>
          {/* Placeholder for Recharts/Chart.js Line Chart */}
          <div className="flex-1 flex items-end relative overflow-hidden pb-6">
             <div className="absolute inset-0 bg-gradient-to-t from-yellow-900/10 to-transparent opacity-50" />
             {/* Simulated Chart Line */}
             <svg viewBox="0 0 100 40" preserveAspectRatio="none" className="w-full h-full stroke-yellow-600 fill-none opacity-80" strokeWidth="0.5">
               <path d="M0,35 Q10,34 20,32 T40,28 T60,25 T80,18 T100,10" />
             </svg>
             {/* X-Axis Labels */}
             <div className="absolute bottom-0 w-full flex justify-between text-[10px] text-zinc-600 pt-2">
                <span>May 2025</span><span>Jul 2025</span><span>Sep 2025</span>
                <span>Nov 2025</span><span>Jan 2026</span><span>Mar 2026</span>
             </div>
          </div>
        </div>

        {/* Stock by Category - Spans 1 column */}
        <div className="bg-[#111111] border border-zinc-800/80 rounded-xl p-5 min-h-[300px] flex flex-col">
          <h3 className="text-sm font-medium text-zinc-300 mb-6">Stock by Category</h3>
          <div className="flex-1 flex flex-col items-center justify-center">
            {/* Simulated Donut Chart using CSS conic-gradient */}
            <div className="relative w-48 h-48 rounded-full bg-zinc-800 mb-6"
                 style={{
                   background: 'conic-gradient(#3b82f6 0% 25%, #8b5cf6 25% 45%, #ec4899 45% 75%, #f59e0b 75% 90%, #ef4444 90% 100%)'
                 }}>
              <div className="absolute inset-4 rounded-full bg-[#111111]" />
            </div>
            
            {/* Legend */}
            <div className="flex flex-wrap justify-center gap-x-4 gap-y-2 text-[10px] text-zinc-500">
              <span className="flex items-center gap-1"><span className="w-1.5 h-1.5 rounded-full bg-blue-500"></span> Electronics</span>
              <span className="flex items-center gap-1"><span className="w-1.5 h-1.5 rounded-full bg-purple-500"></span> Office Supplies</span>
              <span className="flex items-center gap-1"><span className="w-1.5 h-1.5 rounded-full bg-pink-500"></span> Raw Materials</span>
              <span className="flex items-center gap-1"><span className="w-1.5 h-1.5 rounded-full bg-yellow-500"></span> Packaging</span>
            </div>
          </div>
        </div>

      </div>

      {/* Demand Forecast Chart */}
      <div className="bg-[#111111] border border-zinc-800/80 rounded-xl p-5 min-h-[300px] flex flex-col">
        <h3 className="text-sm font-medium text-zinc-300 mb-6">Demand Forecast</h3>
         <div className="flex-1 flex items-end relative overflow-hidden pb-6">
            {/* Simulated Chart Line */}
            <svg viewBox="0 0 100 40" preserveAspectRatio="none" className="w-full h-full fill-none opacity-80" strokeWidth="0.5">
              <path d="M0,25 L15,24 L30,26 L45,22 L60,20" className="stroke-yellow-600" />
              <path d="M60,20 L75,18 L90,17 L100,16" className="stroke-blue-500 stroke-dasharray-[1_1]" strokeDasharray="1,1" />
            </svg>
            {/* X-Axis Labels */}
            <div className="absolute bottom-0 w-full flex justify-between text-[10px] text-zinc-600 pt-2">
              <span>Jul</span><span>Aug</span><span>Sep</span><span>Oct</span><span>Nov</span>
              <span>Dec</span><span>Jan</span><span>Feb</span><span>Mar</span><span>Apr</span><span>May</span><span>Jun</span>
            </div>
         </div>
         {/* Chart Legend */}
         <div className="flex justify-center gap-4 text-[10px] text-zinc-500 mt-2">
            <span className="flex items-center gap-1"><span className="w-3 h-[2px] bg-yellow-600"></span> Actual</span>
            <span className="flex items-center gap-1"><span className="w-3 h-[2px] border-b border-dashed border-blue-500"></span> Forecast</span>
            <span className="flex items-center gap-1"><span className="w-3 h-[2px] bg-zinc-800"></span> Confidence Band</span>
         </div>
      </div>
      
    </div>
  );
}