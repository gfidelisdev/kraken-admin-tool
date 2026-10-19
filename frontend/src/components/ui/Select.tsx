"use client";

import React from "react";
import { cn } from "@/lib/utils";

export interface SelectProps extends React.SelectHTMLAttributes<HTMLSelectElement> {
  label?: string;
  error?: string;
  options: { label: string; value: string }[];
}

export const Select = React.forwardRef<HTMLSelectElement, SelectProps>(
  ({ className, label, error, options, ...props }, ref) => {
    return (
      <div className="w-full space-y-1.5">
        {label && (
          <label className="text-xs font-medium text-slate-300">{label}</label>
        )}
        <select
          ref={ref}
          className={cn(
            "flex h-9 w-full rounded-lg border border-slate-700 bg-slate-900/90 px-3 py-1 text-sm text-slate-100 shadow-sm transition-colors",
            "focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-sky-500 focus-visible:border-sky-500",
            "disabled:cursor-not-allowed disabled:opacity-50",
            error ? "border-rose-500" : "",
            className
          )}
          {...props}
        >
          {options.map((opt) => (
            <option key={opt.value} value={opt.value} className="bg-slate-900 text-slate-100">
              {opt.label}
            </option>
          ))}
        </select>
        {error && <p className="text-xs text-rose-400">{error}</p>}
      </div>
    );
  }
);

Select.displayName = "Select";
