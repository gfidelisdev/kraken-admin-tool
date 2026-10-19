"use client";

import React from "react";
import { cn } from "@/lib/utils";

export function Table({ className, ...props }: React.HTMLAttributes<HTMLTableElement>) {
  return (
    <div className="relative w-full overflow-auto rounded-xl border border-slate-800 bg-slate-900/50 backdrop-blur-sm">
      <table className={cn("w-full caption-bottom text-sm text-left", className)} {...props} />
    </div>
  );
}

export function TableHeader({
  className,
  ...props
}: React.HTMLAttributes<HTMLTableSectionElement>) {
  return <thead className={cn("[&_tr]:border-b border-slate-800 bg-slate-950/40", className)} {...props} />;
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
