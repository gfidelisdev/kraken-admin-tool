"use client";

import React from "react";
import { cn } from "@/lib/utils";
import { ArrowUpDown, ArrowUp, ArrowDown } from "lucide-react";

export function Table({
  className,
  "aria-label": ariaLabel = "Data table",
  ...props
}: React.HTMLAttributes<HTMLTableElement> & { "aria-label"?: string }) {
  return (
    <div
      tabIndex={0}
      role="region"
      aria-label={ariaLabel}
      className="relative w-full overflow-auto rounded-xl border border-slate-800 bg-slate-900/50 backdrop-blur-sm focus:outline-none focus:ring-1 focus:ring-sky-500/50"
    >
      <table className={cn("w-full caption-bottom text-sm text-left", className)} {...props} />
    </div>
  );
}

export function TableHeader({
  className,
  ...props
}: React.HTMLAttributes<HTMLTableSectionElement>) {
  return (
    <thead
      className={cn(
        "sticky top-0 z-10 [&_tr]:border-b border-slate-800 bg-slate-950/70 backdrop-blur-md",
        className
      )}
      {...props}
    />
  );
}

export function TableBody({
  className,
  ...props
}: React.HTMLAttributes<HTMLTableSectionElement>) {
  return <tbody className={cn("[&_tr:last-child]:border-0", className)} {...props} />;
}

export function TableRow({
  className,
  ...props
}: React.HTMLAttributes<HTMLTableRowElement>) {
  return (
    <tr
      className={cn(
        "border-b border-slate-800/80 transition-colors hover:bg-slate-800/40 data-[state=selected]:bg-slate-800",
        className
      )}
      {...props}
    />
  );
}

export function TableHead({
  className,
  ...props
}: React.ThHTMLAttributes<HTMLTableCellElement>) {
  return (
    <th
      className={cn(
        "h-11 px-4 text-xs font-semibold uppercase tracking-wider text-slate-400 align-middle [&:has([role=checkbox])]:pr-0",
        className
      )}
      {...props}
    />
  );
}

export interface SortableTableHeadProps
  extends React.ThHTMLAttributes<HTMLTableCellElement> {
  sortKey: string;
  currentSortKey?: string | null;
  currentDirection?: "asc" | "desc";
  onSort?: (key: string) => void;
  align?: "left" | "right" | "center";
}

export function SortableTableHead({
  className,
  children,
  sortKey,
  currentSortKey,
  currentDirection = "asc",
  onSort,
  align = "left",
  ...props
}: SortableTableHeadProps) {
  const isSorted = currentSortKey === sortKey;
  const ariaSort = isSorted
    ? currentDirection === "asc"
      ? "ascending"
      : "descending"
    : "none";

  return (
    <th
      className={cn(
        "h-11 px-4 text-xs font-semibold uppercase tracking-wider text-slate-400 align-middle select-none",
        align === "right" && "text-right",
        align === "center" && "text-center",
        className
      )}
      aria-sort={ariaSort}
      {...props}
    >
      {onSort ? (
        <button
          type="button"
          onClick={() => onSort(sortKey)}
          aria-label={`Ordenar por ${typeof children === "string" ? children : sortKey}, atualmente ${
            isSorted ? (currentDirection === "asc" ? "crescente" : "decrescente") : "sem ordenação"
          }`}
          className={cn(
            "group inline-flex items-center gap-1.5 transition-colors focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-sky-500 rounded py-1 px-1 -mx-1",
            align === "right" && "ml-auto flex-row-reverse",
            align === "center" && "mx-auto justify-center",
            isSorted ? "text-sky-400 font-bold" : "hover:text-slate-200"
          )}
        >
          <span>{children}</span>
          <span className="shrink-0">
            {isSorted ? (
              currentDirection === "asc" ? (
                <ArrowUp className="h-3.5 w-3.5 text-sky-400" />
              ) : (
                <ArrowDown className="h-3.5 w-3.5 text-sky-400" />
              )
            ) : (
              <ArrowUpDown className="h-3.5 w-3.5 text-slate-500 opacity-40 group-hover:opacity-100 transition-opacity" />
            )}
          </span>
        </button>
      ) : (
        children
      )}
    </th>
  );
}

export function TableCell({
  className,
  ...props
}: React.TdHTMLAttributes<HTMLTableCellElement>) {
  return (
    <td
      className={cn("p-4 align-middle text-slate-200 [&:has([role=checkbox])]:pr-0", className)}
      {...props}
    />
  );
}

export function TableEmpty({
  colSpan,
  message = "No records found.",
  icon,
}: {
  colSpan: number;
  message?: string;
  icon?: React.ReactNode;
}) {
  return (
    <TableRow>
      <TableCell colSpan={colSpan} className="h-40 text-center text-slate-400">
        <div className="flex flex-col items-center justify-center space-y-2">
          {icon && <div className="text-slate-500 mb-1">{icon}</div>}
          <p className="text-sm font-medium">{message}</p>
        </div>
      </TableCell>
    </TableRow>
  );
}
