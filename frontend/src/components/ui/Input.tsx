"use client";

import React from "react";
import { cn } from "@/lib/utils";

export interface InputProps extends React.InputHTMLAttributes<HTMLInputElement> {
  error?: string;
  icon?: React.ReactNode;
}

export const Input = React.forwardRef<HTMLInputElement, InputProps>(
  ({ className, type, error, icon, ...props }, ref) => {
    return (
      <div className="w-full">
        <div className="relative flex items-center">
          {icon && (
            <div className="absolute left-3 text-slate-400 pointer-events-none flex items-center">
              {icon}
            </div>
          )}
          <input
            type={type}
            className={cn(
              "flex h-9 w-full rounded-lg border border-slate-700 bg-slate-900/80 px-3 py-1 text-sm text-slate-100 placeholder:text-slate-500 shadow-sm transition-colors",
              "focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-sky-500 focus-visible:border-sky-500",
              "disabled:cursor-not-allowed disabled:opacity-50",
              icon ? "pl-9" : "",
              error ? "border-rose-500 focus-visible:ring-rose-500 focus-visible:border-rose-500" : "",
              className
            )}
            ref={ref}
            {...props}
          />
        </div>
        {error && <p className="mt-1 text-xs text-rose-400">{error}</p>}
      </div>
    );
  }
);

Input.displayName = "Input";
