import React from 'react';

interface MetricCardProps {
  title: string;
  value: string;
  description: string;
  icon: React.ReactNode;
}

export const MetricCard: React.FC<MetricCardProps> = ({ title, value, description, icon }) => {
  return (
    <div className="bg-[#111111] border border-zinc-800/80 rounded-xl p-5 flex flex-col gap-3">
      <div className="flex items-center gap-3">
        <div className="h-8 w-8 rounded-lg bg-yellow-900/20 text-yellow-600 flex items-center justify-center">
          {icon}
        </div>
        <span className="text-sm font-medium text-zinc-400">{title}</span>
      </div>
      <div>
        <h2 className="text-2xl font-bold text-zinc-100">{value}</h2>
        <p className="text-xs text-zinc-500 mt-1">{description}</p>
      </div>
    </div>
  );
};