"use client";

import React, { useState, useRef, useEffect } from "react";
import { Moon, Sun, Contrast, Palette, Sunset, Check, ChevronDown } from "lucide-react";
import { useTheme, Theme, ThemeOption } from "@/context/ThemeContext";

export function ThemeSelector() {
  const { theme, setTheme, themes } = useTheme();
  const [isOpen, setIsOpen] = useState(false);
  const dropdownRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    function handleClickOutside(event: MouseEvent) {
      if (dropdownRef.current && !dropdownRef.current.contains(event.target as Node)) {
        setIsOpen(false);
      }
    }
    document.addEventListener("mousedown", handleClickOutside);
    return () => document.removeEventListener("mousedown", handleClickOutside);
  }, []);

  const getThemeIcon = (id: Theme) => {
    switch (id) {
      case "light":
        return <Sun className="h-4 w-4 text-amber-500" />;
      case "high-contrast":
        return <Contrast className="h-4 w-4 text-yellow-400" />;
      case "colorblind":
        return <Palette className="h-4 w-4 text-sky-400" />;
      case "sepia":
        return <Sunset className="h-4 w-4 text-amber-500" />;
      case "dark":
      default:
        return <Moon className="h-4 w-4 text-sky-400" />;
    }
  };

  const currentTheme = themes.find((t) => t.id === theme) || themes[0];

  return (
    <div className="relative" ref={dropdownRef}>
      <button
        type="button"
        onClick={() => setIsOpen((prev) => !prev)}
        aria-label={`Select theme, current theme is ${currentTheme.name}`}
        aria-expanded={isOpen}
        title={`Theme: ${currentTheme.name}`}
        className="flex items-center gap-2 rounded-xl border border-slate-800 bg-slate-900/80 px-2.5 py-1.5 text-xs text-slate-300 hover:border-slate-700 hover:text-white transition-colors"
      >
        <div className="flex items-center justify-center">
          {getThemeIcon(theme)}
        </div>
        <span className="hidden lg:inline text-xs font-medium">
          {currentTheme.name}
        </span>
        <ChevronDown className="h-3 w-3 text-slate-400" />
      </button>

      {isOpen && (
        <div className="absolute right-0 mt-2 w-64 rounded-xl border border-slate-800 bg-slate-900 p-2 shadow-2xl z-50 animate-in fade-in zoom-in-95">
          <div className="px-3 py-1.5 border-b border-slate-800/80 mb-1">
            <p className="text-[10px] font-semibold uppercase tracking-wider text-slate-400">
              Appearance & Contrast
            </p>
          </div>

          <div className="space-y-1">
            {themes.map((t: ThemeOption) => {
              const isSelected = t.id === theme;
              return (
                <button
                  key={t.id}
                  type="button"
                  onClick={() => {
                    setTheme(t.id);
                    setIsOpen(false);
                  }}
                  className={`w-full flex items-start gap-2.5 rounded-lg p-2 text-left transition-colors ${
                    isSelected
                      ? "bg-slate-800 text-white font-medium"
                      : "text-slate-300 hover:bg-slate-800/60 hover:text-white"
                  }`}
                >
                  <div className="p-1 rounded bg-slate-950/60 border border-slate-800 mt-0.5 shrink-0">
                    {getThemeIcon(t.id)}
                  </div>

                  <div className="flex-1 min-w-0">
                    <div className="flex items-center justify-between">
                      <span className="text-xs font-medium">{t.name}</span>
                      {isSelected && (
                        <Check className="h-3.5 w-3.5 text-sky-400" />
                      )}
                    </div>
                    <p className="text-[11px] text-slate-400 leading-tight mt-0.5">
                      {t.description}
                    </p>
                  </div>
                </button>
              );
            })}
          </div>
        </div>
      )}
    </div>
  );
}
