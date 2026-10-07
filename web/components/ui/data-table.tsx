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

  const activeSearchCol =
    searchColumn ??
    (table.getColumn("name")
      ? "name"
      : table.getColumn("displayName")
      ? "displayName"
      : undefined);

  return (
    <div className="overflow-hidden border border-zinc-800 bg-[#0c0c0c] shadow-sm rounded-none w-full">
      <div className="flex flex-col sm:flex-row sm:items-center justify-between p-3 sm:p-4 border-b border-zinc-800/80 gap-3">
        <div className="flex items-center gap-3 flex-1 w-full">
          {customSearch ? (
            customSearch
          ) : onSearchChange ? (
            <Input
              placeholder={searchPlaceholder ?? "Search inventory..."}
              value={searchValue ?? ""}
              onChange={(event) => onSearchChange(event.target.value)}
              className="max-w-sm bg-zinc-900/60 border-zinc-800 text-zinc-100 placeholder:text-zinc-500 h-9 text-sm"
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
              className="max-w-sm bg-zinc-900/60 border-zinc-800 text-zinc-100 placeholder:text-zinc-500 h-9 text-sm"
            />
          ) : null}
          {filterComponent}
        </div>

        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button
              variant="outline"
              size="sm"
              className="ml-auto border-zinc-800 bg-zinc-900/50 text-zinc-300 hover:text-white hover:bg-zinc-800 gap-2"
            >
              <SlidersHorizontal className="h-3.5 w-3.5" />
              Columns
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent
            align="end"
            className="bg-zinc-900 border-zinc-800 text-zinc-300"
          >
            {table
              .getAllColumns()
              .filter((column) => column.getCanHide())
              .map((column) => (
                <DropdownMenuCheckboxItem
                  key={column.id}
                  className="capitalize text-zinc-300 focus:bg-zinc-800 focus:text-white text-xs"
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

      <div className="overflow-x-auto">
        <Table>
          <TableHeader className="bg-zinc-900/40 border-b border-zinc-800">
            {table.getHeaderGroups().map((headerGroup) => (
              <TableRow
                key={headerGroup.id}
                className="border-zinc-800 hover:bg-transparent"
              >
                {headerGroup.headers.map((header) => (
                  <TableHead
                    key={header.id}
                    className="text-xs uppercase tracking-wider text-zinc-400 font-semibold py-3 px-4"
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
                    <TableCell key={cell.id} className="py-3.5 px-4 text-sm">
                      <table.FlexRender cell={cell} />
                    </TableCell>
                  ))}
                </TableRow>
              ))
            ) : (
              <TableRow>
                <TableCell
                  colSpan={columns.length}
                  className="h-24 text-center text-zinc-500 text-sm"
                >
                  No results found.
                </TableCell>
              </TableRow>
            )}
          </TableBody>
        </Table>
      </div>

      {pagination && (
        <div className="flex items-center justify-between px-4 py-3 border-t border-zinc-800/80 text-xs text-zinc-400">
          <div className="flex items-center gap-2">
            <span>Rows per page:</span>
            <select
              value={pagination.limit || 20}
              onChange={(e) => pagination.onLimitChange?.(Number(e.target.value))}
              className="bg-zinc-900 border border-zinc-800 rounded-none px-2 py-1 text-zinc-200 text-xs focus:outline-none focus:border-zinc-700 font-mono"
            >
              <option value={10}>10</option>
              <option value={20}>20</option>
              <option value={50}>50</option>
              <option value={100}>100</option>
            </select>
            <span className="text-zinc-500 ml-2 font-mono">
              Showing {data.length} records
            </span>
          </div>

          <div className="flex items-center gap-2">
            <Button
              variant="outline"
              size="sm"
              disabled={!pagination.canPrev}
              onClick={pagination.onPrev}
              className="h-8 px-2.5 rounded-none border-zinc-800 bg-zinc-900/40 text-zinc-300 hover:text-white hover:bg-zinc-800 disabled:opacity-40"
            >
              <ChevronLeft className="h-4 w-4 mr-1" />
              Previous
            </Button>
            <Button
              variant="outline"
              size="sm"
              disabled={!pagination.hasMore}
              onClick={pagination.onNext}
              className="h-8 px-2.5 rounded-none border-zinc-800 bg-zinc-900/40 text-zinc-300 hover:text-white hover:bg-zinc-800 disabled:opacity-40"
            >
              Next
              <ChevronRight className="h-4 w-4 ml-1" />
            </Button>
          </div>
        </div>
      )}
    </div>
  );
}

