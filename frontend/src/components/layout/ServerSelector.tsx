"use client";

import React, { useState, useRef, useEffect } from "react";
import Link from "next/link";
import { Server, ChevronDown, Check, Plus, Settings2, Globe, Radio } from "lucide-react";
import { useServer } from "@/context/ServerContext";

export function ServerSelector() {
  const { servers, activeServer, setActiveServer } = useServer();
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

  const handleSelect = (server: any) => {
    setActiveServer(server);
    setIsOpen(false);
    // Reload active page data to refresh container/image feeds
    if (typeof window !== "undefined") {
      window.dispatchEvent(new CustomEvent("kraken:server-changed", { detail: server }));
    }
  };

  const isConnected = activeServer?.status === "CONNECTED";

  return (
    <div ref={dropdownRef} className="relative">
      <button
        type="button"
        onClick={() => setIsOpen((prev) => !prev)}
        className="flex items-center space-x-2.5 rounded-xl border border-slate-800 bg-slate-900/90 px-3 py-1.5 text-left transition-colors hover:border-slate-700 hover:bg-slate-800/80"
      >
        <div className="relative flex items-center justify-center">
          <Server className="h-4 w-4 text-sky-400" />
          <span
            className={`absolute -bottom-0.5 -right-0.5 h-2 w-2 rounded-full border border-slate-900 ${
              isConnected ? "bg-emerald-400" : "bg-rose-400"
            }`}
          />
        </div>

        <div className="hidden sm:block text-left">
          <div className="flex items-center space-x-1.5">
            <span className="text-xs font-semibold text-slate-200 leading-none">
              {activeServer?.name || "Local Host"}
            </span>
            {activeServer?.is_default && (
              <span className="rounded bg-sky-500/10 border border-sky-500/20 px-1 py-0.2 text-[9px] font-mono text-sky-400">
                Default
              </span>
            )}
          </div>
          <p className="text-[10px] text-slate-400 leading-none mt-1 font-mono truncate max-w-[130px]">
            {activeServer?.host_url || "Connecting..."}
          </p>
        </div>

        <ChevronDown className="h-3.5 w-3.5 text-slate-400 shrink-0" />
      </button>

      {/* Dropdown Menu */}
      {isOpen && (
        <div className="absolute left-0 sm:left-auto sm:right-0 mt-2 w-72 rounded-xl border border-slate-800 bg-slate-900/95 p-1.5 shadow-2xl backdrop-blur-md z-50 animate-in fade-in zoom-in-95">
          <div className="flex items-center justify-between px-2.5 py-1.5 border-b border-slate-800 text-[10px] font-semibold uppercase tracking-wider text-slate-400">
            <span>Docker Hosts ({servers.length})</span>
            <Link
              href="/servers"
              onClick={() => setIsOpen(false)}
              className="text-sky-400 hover:text-sky-300 flex items-center space-x-1"
            >
              <Settings2 className="h-3 w-3" />
              <span>Manage</span>
            </Link>
          </div>

          <div className="max-h-64 overflow-y-auto divide-y divide-slate-800/40 my-1">
            {servers.map((server) => {
              const isSelected = activeServer?.id === server.id;
              const serverOnline = server.status === "CONNECTED";

              return (
                <button
                  key={server.id}
                  type="button"
                  onClick={() => handleSelect(server)}
                  className={`w-full rounded-lg px-2.5 py-2 text-left transition-colors flex items-start justify-between gap-2 group ${
                    isSelected ? "bg-slate-800/80" : "hover:bg-slate-800/40"
                  }`}
                >
                  <div className="min-w-0 flex-1">
                    <div className="flex items-center space-x-1.5">
                      <span
                        className={`h-2 w-2 rounded-full shrink-0 ${
                          serverOnline ? "bg-emerald-400" : "bg-rose-400"
                        }`}
                      />
                      <span className="font-semibold text-xs text-white truncate">
                        {server.name}
                      </span>
                      {server.is_default && (
                        <span className="text-[9px] px-1 py-0.2 rounded bg-slate-800 text-slate-300 border border-slate-700">
                          Default
                        </span>
                      )}
                    </div>
                    <p className="mt-0.5 font-mono text-[10px] text-slate-400 truncate">
                      {server.host_url}
                    </p>
                    {server.version && (
                      <p className="text-[10px] text-slate-400 font-mono">
                        Docker {server.version}
                      </p>
                    )}
                  </div>

                  {isSelected && (
                    <Check className="h-4 w-4 text-sky-400 shrink-0 mt-0.5" />
                  )}
                </button>
              );
            })}
          </div>

          <div className="pt-1 border-t border-slate-800">
            <Link
              href="/servers"
              onClick={() => setIsOpen(false)}
              className="flex items-center justify-center space-x-1.5 w-full rounded-lg py-1.5 text-xs font-medium text-sky-400 hover:bg-slate-800/60 transition-colors"
            >
              <Plus className="h-3.5 w-3.5" />
              <span>Add Remote Host</span>
            </Link>
          </div>
        </div>
      )}
    </div>
  );
}
