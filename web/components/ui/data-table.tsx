"use client";

import React, { useState } from "react";
import {
  useTable,
  type ColumnDef,
  type RowData,
  type SortingState,
} from "@tanstack/react-table";

import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  features,
  type DataTableFeatures,
  type ColumnFiltersState,
  type ColumnVisibilityState,
} from "./data-table-features";
import {
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { SlidersHorizontal, ChevronLeft, ChevronRight, Loader2 } from "lucide-react";
import { cn } from "cn";

export interface DataTablePaginationProps {
  hasMore?: boolean;
  loadingMore?: boolean;
  onNext?: () => void;
  onPrev?: () => void;
  canPrev?: boolean;
  limit?: number;
  onLimitChange?: (limit: number) => void;
  total?: number;
  onLoadAll?: () => void;
  loadingAll?: boolean;
  entityName?: string;
}

export interface DataTableProps<TData extends RowData> {
  columns: ColumnDef<DataTableFeatures, TData, any>[];
  data: TData[];
  searchColumn?: string;
  searchPlaceholder?: string;
  onRowClick?: (row: TData) => void;
  pagination?: DataTablePaginationProps;
  searchValue?: string;
  onSearchChange?: (val: string) => void;
  filterComponent?: React.ReactNode;
  customSearch?: React.ReactNode;
  tableContainerClassName?: string;
}

export function DataTable<TData extends RowData>({
  columns,
  data,
  searchColumn,
  searchPlaceholder,
  onRowClick,
  pagination,
  searchValue,
  onSearchChange,
  filterComponent,
  customSearch,
  tableContainerClassName,
}: DataTableProps<TData>) {
  const [sorting, setSorting] = useState<SortingState>([]);
  const [columnFilters, setColumnFilters] = useState<ColumnFiltersState>([]);
  const [columnVisibility, setColumnVisibility] =
    useState<ColumnVisibilityState>({});

  const table = useTable({
    features,
    data,
    columns,
    onSortingChange: setSorting,
    onColumnFiltersChange: setColumnFilters,
    onColumnVisibilityChange: setColumnVisibility,
    state: {
      sorting,
      columnFilters,
      columnVisibility,
    },
  });

  const viewportRef = React.useRef<HTMLDivElement>(null);
  const sentinelRef = React.useRef<HTMLTableRowElement>(null);

  // Auto-load next page on scroll via IntersectionObserver
  React.useEffect(() => {
    if (!pagination?.hasMore || pagination?.loadingMore || !pagination?.onNext) {
      return;
    }

    const currentSentinel = sentinelRef.current;
    if (!currentSentinel) return;

    const observer = new IntersectionObserver(
      (entries) => {
        const [entry] = entries;
        if (entry && entry.isIntersecting) {
          pagination.onNext?.();
        }
      },
      {
        root: viewportRef.current,
        rootMargin: "300px",
        threshold: 0,
      }
    );

    observer.observe(currentSentinel);

    return () => {
      observer.disconnect();
    };
  }, [pagination?.hasMore, pagination?.loadingMore, pagination?.onNext]);

  // Fallback scroll listener for rapid / flick scrolling
  const handleScroll = (e: React.UIEvent<HTMLDivElement>) => {
    if (!pagination?.hasMore || pagination?.loadingMore || !pagination?.onNext) {
      return;
    }
    const el = e.currentTarget;
    if (el.scrollHeight - el.scrollTop - el.clientHeight < 350) {
      pagination.onNext();
    }
  };

  // Only show column search if explicitly requested
  const activeSearchCol = searchColumn;

  return (
    <div className="overflow-hidden border border-zinc-800 bg-[#0c0c0c] shadow-sm rounded-none w-full">
      {/* Table Header Bar (Filters & Column Visibility) */}
      <div className="flex flex-col sm:flex-row sm:items-center justify-between p-2.5 sm:p-3 border-b border-zinc-800/80 gap-2.5">
        <div className="flex items-center gap-2.5 flex-1 w-full">
          {customSearch ? (
            customSearch
          ) : onSearchChange ? (
            <Input
              placeholder={searchPlaceholder ?? "Search..."}
              value={searchValue ?? ""}
              onChange={(event) => onSearchChange(event.target.value)}
              className="max-w-xs bg-zinc-900/60 border-zinc-800 text-zinc-100 placeholder:text-zinc-500 h-8 text-xs rounded-none"
            />
          ) : activeSearchCol ? (
            <Input
              placeholder={searchPlaceholder ?? `Filter by ${activeSearchCol}...`}
              value={
                (table.getColumn(activeSearchCol)?.getFilterValue() as string) ?? ""
              }
              onChange={(event) =>
                table.getColumn(activeSearchCol)?.setFilterValue(event.target.value)
              }
              className="max-w-xs bg-zinc-900/60 border-zinc-800 text-zinc-100 placeholder:text-zinc-500 h-8 text-xs rounded-none"
            />
          ) : null}
          {filterComponent}
        </div>

        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button
              variant="outline"
              size="sm"
              className="ml-auto border-zinc-800 bg-zinc-900/50 text-zinc-300 hover:text-white hover:bg-zinc-800 gap-1.5 h-8 text-xs rounded-none"
            >
              <SlidersHorizontal className="h-3.5 w-3.5" />
              Columns
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent
            align="end"
            className="bg-zinc-900 border-zinc-800 text-zinc-300 rounded-none"
          >
            {table
              .getAllColumns()
              .filter((column) => column.getCanHide())
              .map((column) => (
                <DropdownMenuCheckboxItem
                  key={column.id}
                  className="capitalize text-zinc-300 focus:bg-zinc-800 focus:text-white text-xs rounded-none"
                  checked={column.getIsVisible()}
                  onCheckedChange={(value) =>
                    column.toggleVisibility(!!value)
                  }
                >
                  {column.id}
                </DropdownMenuCheckboxItem>
              ))}
          </DropdownMenuContent>
        </DropdownMenu>
      </div>

      {/* Scrollable Table Viewport */}
      <div
        ref={viewportRef}
        onScroll={handleScroll}
        className={cn(
          "overflow-x-auto overflow-y-auto",
          tableContainerClassName ?? "max-h-[calc(100vh-230px)] min-h-[400px]"
        )}
      >
        <Table noContainer>
          <TableHeader className="bg-[#0e0e0e] sticky top-0 z-10 border-b border-zinc-800">
            {table.getHeaderGroups().map((headerGroup) => (
              <TableRow
                key={headerGroup.id}
                className="border-zinc-800 hover:bg-transparent"
              >
                {headerGroup.headers.map((header) => (
                  <TableHead
                    key={header.id}
                    className="text-xs uppercase tracking-wider text-zinc-400 font-semibold py-2.5 px-3.5 font-mono bg-[#0e0e0e]"
                  >
                    {header.isPlaceholder ? null : (
                      <table.FlexRender header={header} />
                    )}
                  </TableHead>
                ))}
              </TableRow>
            ))}
          </TableHeader>
          <TableBody>
            {table.getRowModel().rows?.length ? (
              <>
                {table.getRowModel().rows.map((row) => (
                  <TableRow
                    key={row.id}
                    data-state={row.getIsSelected() && "selected"}
                    onClick={() => onRowClick?.(row.original)}
                    tabIndex={onRowClick ? 0 : undefined}
                    role={onRowClick ? "button" : undefined}
                    onKeyDown={
                      onRowClick
                        ? (e) => {
                            if (e.key === "Enter" || e.key === " ") {
                              e.preventDefault();
                              onRowClick(row.original);
                            }
                          }
                        : undefined
                    }
                    className={`border-b border-zinc-800/60 transition-colors focus:outline-none focus:bg-zinc-800/80 ${
                      onRowClick ? "cursor-pointer hover:bg-zinc-800/60" : "hover:bg-zinc-900/40"
                    }`}
                  >
                    {row.getVisibleCells().map((cell) => (
                      <TableCell key={cell.id} className="py-2.5 px-3.5 text-xs">
                        <table.FlexRender cell={cell} />
                      </TableCell>
                    ))}
                  </TableRow>
                ))}
                {/* Sentinel row for IntersectionObserver */}
                {pagination?.hasMore && (
                  <tr ref={sentinelRef} className="h-1 border-none">
                    <td colSpan={columns.length} className="p-0 border-none" />
                  </tr>
                )}
                {/* Visual loading indicator while fetching next batch */}
                {pagination?.loadingMore && (
                  <TableRow className="border-none hover:bg-transparent">
                    <TableCell
                      colSpan={columns.length}
                      className="py-3 text-center text-xs font-mono text-yellow-400 bg-zinc-950/80 border-t border-zinc-800/50"
                    >
                      <div className="flex items-center justify-center gap-2">
                        <Loader2 className="h-3.5 w-3.5 animate-spin text-yellow-500" />
                        <span>Loading more {pagination.entityName || "records"}...</span>
                      </div>
                    </TableCell>
                  </TableRow>
                )}
              </>
            ) : (
              <TableRow>
                <TableCell
                  colSpan={columns.length}
                  className="h-24 text-center text-zinc-500 text-xs font-mono"
                >
                  No results found.
                </TableCell>
              </TableRow>
            )}
          </TableBody>
        </Table>
      </div>

      {/* Scrollable Status & Show More Footer */}
      {pagination && (
        <div className="flex items-center justify-between px-4 py-2 border-t border-zinc-800/80 text-xs text-zinc-400 bg-[#0a0a0a]">
          <div className="flex items-center gap-3 font-mono text-[11px]">
            <span className="text-zinc-400">
              Showing <span className="text-white font-semibold">{data.length}</span>
              {pagination.total ? (
                <> of <span className="text-white font-semibold">{pagination.total}</span></>
              ) : ""}{" "}
              {pagination.entityName || "records"}
            </span>
            {pagination.loadingMore && (
              <span className="flex items-center gap-1.5 text-yellow-400 font-medium">
                <Loader2 className="h-3 w-3 animate-spin" />
                Scrolling / loading more...
              </span>
            )}
          </div>

          <div className="flex items-center gap-2">
            {pagination.hasMore ? (
              <>
                <button
                  type="button"
                  onClick={pagination.onNext}
                  disabled={pagination.loadingMore || pagination.loadingAll}
                  className="px-2.5 py-1 bg-zinc-900 border border-zinc-700 hover:border-yellow-600 hover:bg-zinc-800 text-yellow-400 text-xs font-mono font-medium transition-colors disabled:opacity-50"
                >
                  {pagination.loadingMore ? "Loading..." : "Show more"}
                </button>
                {pagination.onLoadAll && (
                  <button
                    type="button"
                    onClick={pagination.onLoadAll}
                    disabled={pagination.loadingMore || pagination.loadingAll}
                    className="px-2.5 py-1 bg-zinc-900 border border-zinc-800 hover:border-zinc-600 hover:bg-zinc-800 text-zinc-300 hover:text-white text-xs font-mono transition-colors disabled:opacity-50"
                  >
                    {pagination.loadingAll ? "Loading all..." : "Load all"}
                  </button>
                )}
              </>
            ) : (
              <span className="text-[11px] font-mono text-emerald-400 flex items-center gap-1.5">
                <span className="w-1.5 h-1.5 bg-emerald-400 inline-block" />
                All {pagination.total || data.length} records loaded
              </span>
            )}
          </div>
        </div>
      )}
    </div>
  );
}

