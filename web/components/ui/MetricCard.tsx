import React from "react";

interface MetricCardProps {
  title: string;
  value: string;
  description: string;
  icon?: React.ReactNode;
}

export const MetricCard: React.FC<MetricCardProps> = ({
  title,
  value,
  description,
  icon,
}) => {
  return (
    <div className="bg-[#09090b] border border-zinc-800/60 rounded-none p-5 flex flex-col justify-between gap-3 min-w-[280px]">
      {/* Header: Title & Optional Icon */}
      <div className="flex items-center justify-between gap-2">
        <span className="text-sm font-semibold text-zinc-100 tracking-tight">
          {title}
        </span>
        {icon && (
          <div className="text-zinc-400 h-5 w-5 flex items-center justify-center shrink-0">
            {icon}
          </div>
        )}
      </div>

      {/* Main Content: Value & Description */}
      <div className="flex flex-col gap-1.5">
        <h2 className="text-3xl font-extrabold text-white tracking-tight">
          {value}
        </h2>
        <p className="text-xs text-zinc-400 leading-relaxed font-normal">
          {description}
        </p>
      </div>
    </div>
  );
};