"use client";

import { Fragment } from 'react';
import { usePathname } from 'next/navigation';
import { Sidebar, Search, Plus, Menu, RefreshCw } from 'lucide-react';

export const Topbar = () => {
  const pathname = usePathname();
  
  // Safely split the URL path into an array, removing empty strings
  const segments = pathname ? pathname.split('/').filter(Boolean) : [];

  // Helper to format paths (e.g., "data-sources" -> "Data Sources")
  const formatLabel = (text: string) => {
    return text
      .split('-')
      .map((word) => word.charAt(0).toUpperCase() + word.slice(1))
      .join(' ');
  };

  return (
    <header className="h-14 border-b border-zinc-900 bg-[#0a0a0a] flex items-center justify-between px-6 shrink-0">
      <div className="flex items-center gap-4">
        <button className="text-zinc-500 hover:text-zinc-300 transition-colors">
          <Sidebar className="h-5 w-5" />
        </button>
        
        {/* Dynamic Breadcrumbs */}
        <div className="flex items-center text-sm text-zinc-500">
          {segments.length === 0 ? (
            <span className="text-zinc-200 font-medium">Dashboard</span>
          ) : (
            segments.map((segment, index) => {
              const isLast = index === segments.length - 1;
              return (
                <Fragment key={`${segment}-${index}`}>
                  <span className={isLast ? "text-zinc-200 font-medium" : "text-zinc-500"}>
                    {formatLabel(segment)}
                  </span>
                  {!isLast && <span className="mx-2 text-zinc-700">›</span>}
                </Fragment>
              );
            })
          )}
        </div>
      </div>
      
      <div className="flex items-center gap-4">
        {/* Search */}
        <div className="relative group">
          <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-zinc-500" />
          <input 
            type="text" 
            placeholder="Search..." 
            className="w-64 bg-zinc-900/50 border border-zinc-800 rounded-md py-1.5 pl-9 pr-12 text-sm text-zinc-200 placeholder:text-zinc-600 focus:outline-none focus:border-zinc-700 transition-colors"
          />
          <div className="absolute right-2 top-1/2 -translate-y-1/2 flex items-center gap-1">
            <kbd className="text-[10px] font-mono text-zinc-500 bg-zinc-800 px-1 rounded">⌘</kbd>
            <kbd className="text-[10px] font-mono text-zinc-500 bg-zinc-800 px-1 rounded">K</kbd>
          </div>
        </div>

        {/* Actions */}
        <div className="flex items-center gap-3 text-zinc-400">
          <button className="hover:text-zinc-200 transition-colors"><Plus className="h-5 w-5" /></button>
          <button className="hover:text-zinc-200 transition-colors"><Menu className="h-5 w-5" /></button>
          <button className="hover:text-zinc-200 transition-colors"><RefreshCw className="h-4 w-4" /></button>
        </div>
      </div>
    </header>
  );
};