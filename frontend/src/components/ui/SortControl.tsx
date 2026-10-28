"use client";

import React from "react";
import { ArrowDown, ArrowUp, ArrowUpDown } from "lucide-react";
import { SortDirection } from "@/lib/sort";

export interface SortOption {
  value: string;
  label: string;
}

interface SortControlProps {
  options: SortOption[];
  currentKey: string;
  currentDirection: SortDirection;
  onKeyChange: (key: string) => void;
  onDirectionToggle: () => void;
  className?: string;
  label?: string;
}

export function SortControl({
  options,
  currentKey,
  currentDirection,
  onKeyChange,
  onDirectionToggle,
  className = "",
  label = "Ordenar:",
}: SortControlProps) {
  return (
    <div className={`flex items-center gap-1.5 text-xs ${className}`}>
      {label && <span className="text-slate-400 whitespace-nowrap">{label}</span>}
      <div className="relative">
        <select
          value={currentKey}
          onChange={(e) => onKeyChange(e.target.value)}
          aria-label="Campo de ordenação"
          className="rounded-lg border border-slate-800 bg-slate-900/90 py-1.5 pl-2.5 pr-8 text-xs text-slate-200 focus:border-sky-500 focus:outline-none focus:ring-1 focus:ring-sky-500 transition-colors cursor-pointer appearance-none"
        >
          {options.map((opt) => (
            <option key={opt.value} value={opt.value}>
              {opt.label}
            </option>
          ))}
        </select>
        <div className="pointer-events-none absolute right-2.5 top-1/2 -translate-y-1/2 text-slate-500">
          <ArrowUpDown className="h-3 w-3" />
        </div>
      </div>

      <button
        type="button"
        onClick={onDirectionToggle}
        title={currentDirection === "asc" ? "Ordem Crescente (clique para decrescente)" : "Ordem Decrescente (clique para crescente)"}
        aria-label={currentDirection === "asc" ? "Ordem Crescente" : "Ordem Decrescente"}
        className="flex items-center justify-center p-1.5 rounded-lg border border-slate-800 bg-slate-900/90 text-slate-300 hover:text-white hover:border-slate-700 transition-colors focus:outline-none focus:ring-1 focus:ring-sky-500"
      >
        {currentDirection === "asc" ? (
          <ArrowUp className="h-3.5 w-3.5 text-sky-400" />
        ) : (
          <ArrowDown className="h-3.5 w-3.5 text-sky-400" />
        )}
      </button>
    </div>
  );
}
