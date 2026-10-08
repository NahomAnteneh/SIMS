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
import { SlidersHorizontal, ChevronLeft, ChevronRight } from "lucide-react";

export interface DataTablePaginationProps {
  hasMore?: boolean;
  onNext?: () => void;
  onPrev?: () => void;
  canPrev?: boolean;
  limit?: number;
  onLimitChange?: (limit: number) => void;
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

  // Only show column search if explicitly requested
  const activeSearchCol = searchColumn;

  const hasSearchInput = Boolean(customSearch || onSearchChange || activeSearchCol);

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
      <div className="overflow-x-auto overflow-y-auto max-h-[calc(100vh-230px)]">
        <Table>
          <TableHeader className="bg-[#0e0e0e] sticky top-0 z-10 border-b border-zinc-800">
            {table.getHeaderGroups().map((headerGroup) => (
              <TableRow
                key={headerGroup.id}
                className="border-zinc-800 hover:bg-transparent"
              >
                {headerGroup.headers.map((header) => (
                  <TableHead
                    key={header.id}
                    className="text-xs uppercase tracking-wider text-zinc-400 font-semibold py-2.5 px-3.5 font-mono"
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
              table.getRowModel().rows.map((row) => (
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
              ))
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
          <div className="flex items-center gap-2">
            <span className="text-zinc-500 font-mono text-[11px]">
              Showing {data.length} records
            </span>
          </div>

          <div>
            {pagination.hasMore ? (
              <button
                type="button"
                onClick={pagination.onNext}
                className="px-3 py-1 bg-zinc-900 border border-zinc-700 hover:border-yellow-600 hover:bg-zinc-800 text-yellow-400 text-xs font-mono font-medium transition-colors"
              >
                Show more
              </button>
            ) : (
              <span className="text-[11px] font-mono text-zinc-600 uppercase tracking-wider">
                All records loaded
              </span>
            )}
          </div>
        </div>
      )}
    </div>
  );
}

