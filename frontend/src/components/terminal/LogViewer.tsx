"use client";

import React, { useState, useRef, useEffect, useMemo } from "react";
import {
  Play,
  Pause,
  Trash2,
  ArrowDown,
  Search,
  Copy,
  Check,
  Terminal as TerminalIcon,
  Wifi,
  WifiOff,
} from "lucide-react";
import { Button } from "@/components/ui/Button";
import { ConnectionStatus } from "@/hooks/useWebSocketLogs";
import { cn } from "@/lib/utils";

interface LogViewerProps {
  logs: string[];
  status: ConnectionStatus;
  isPaused: boolean;
  onTogglePause: () => void;
  onClear: () => void;
  title?: string;
  className?: string;
}

export function LogViewer({
  logs,
  status,
  isPaused,
  onTogglePause,
  onClear,
  title = "Live Terminal Logs",
  className,
}: LogViewerProps) {
  const [filter, setFilter] = useState("");
  const [autoScroll, setAutoScroll] = useState(true);
  const [copied, setCopied] = useState(false);
  const scrollContainerRef = useRef<HTMLDivElement>(null);

  // Filter logs based on search text
  const filteredLogs = useMemo(() => {
    if (!filter.trim()) return logs;
    const lower = filter.toLowerCase();
    return logs.filter((line) => line.toLowerCase().includes(lower));
  }, [logs, filter]);

  // Handle auto-scroll to bottom
  useEffect(() => {
    if (autoScroll && scrollContainerRef.current) {
      scrollContainerRef.current.scrollTop = scrollContainerRef.current.scrollHeight;
    }
  }, [filteredLogs, autoScroll]);

  // Copy logs to clipboard
  const handleCopy = () => {
    const text = filteredLogs.join("\n");
    navigator.clipboard.writeText(text);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  // Helper to colorize log lines based on keywords
  const renderLogLine = (line: string, index: number) => {
    let colorClass = "text-slate-300";
    const upper = line.toUpperCase();

    if (upper.includes("ERROR") || upper.includes("FATAL") || upper.includes("FAIL")) {
      colorClass = "text-rose-400 font-medium";
    } else if (upper.includes("WARN")) {
      colorClass = "text-amber-300";
    } else if (upper.includes("INFO")) {
      colorClass = "text-sky-300";
    } else if (upper.includes("DEBUG") || upper.includes("TRACE")) {
      colorClass = "text-slate-500";
    } else if (upper.includes("SUCCESS") || upper.includes("OK") || upper.includes("200 OK")) {
      colorClass = "text-emerald-400";
    }

    return (
      <div
        key={index}
        className="flex items-start hover:bg-slate-800/40 px-3 py-0.5 group font-mono text-xs leading-5"
      >
        <span className="w-12 select-none text-right pr-4 text-slate-600 group-hover:text-slate-500 shrink-0 font-mono">
          {index + 1}
        </span>
        <span className={cn("break-all whitespace-pre-wrap flex-1", colorClass)}>
          {line}
        </span>
      </div>
    );
  };

  return (
    <div
      className={cn(
        "flex flex-col rounded-xl border border-slate-800 bg-slate-950/95 overflow-hidden shadow-2xl backdrop-blur-md",
        className
      )}
    >
      {/* Terminal Header & Toolbar */}
      <div className="flex flex-wrap items-center justify-between gap-3 border-b border-slate-800/80 bg-slate-900/90 px-4 py-2.5">
        <div className="flex items-center space-x-3">
          <div className="flex space-x-1.5">
            <span className="h-3 w-3 rounded-full bg-rose-500/80" />
            <span className="h-3 w-3 rounded-full bg-amber-500/80" />
            <span className="h-3 w-3 rounded-full bg-emerald-500/80" />
          </div>
          <div className="flex items-center space-x-2 border-l border-slate-800 pl-3">
            <TerminalIcon className="h-4 w-4 text-sky-400" />
            <span className="text-xs font-semibold text-slate-200">{title}</span>
          </div>
          {/* Connection Status Indicator */}
          <div className="flex items-center space-x-1.5 rounded-full bg-slate-950 px-2.5 py-0.5 border border-slate-800 text-[11px]">
            {status === "connected" ? (
              <>
                <span className="h-2 w-2 rounded-full bg-emerald-400 animate-pulse" />
                <span className="text-emerald-400 font-medium">Streaming</span>
              </>
            ) : status === "connecting" ? (
              <>
                <span className="h-2 w-2 rounded-full bg-amber-400 animate-ping" />
                <span className="text-amber-400 font-medium">Connecting...</span>
              </>
            ) : status === "error" ? (
              <>
                <WifiOff className="h-3 w-3 text-rose-400" />
                <span className="text-rose-400 font-medium">Connection Error</span>
              </>
            ) : (
              <>
                <Wifi className="h-3 w-3 text-slate-500" />
                <span className="text-slate-500 font-medium">Disconnected</span>
              </>
            )}
          </div>
        </div>

        {/* Action Controls */}
        <div className="flex items-center space-x-2">
          {/* Search/Filter Bar */}
          <div className="relative">
            <Search className="absolute left-2.5 top-1/2 -translate-y-1/2 h-3.5 w-3.5 text-slate-500" />
            <input
              type="text"
              placeholder="Filter logs..."
              value={filter}
              onChange={(e) => setFilter(e.target.value)}
              className="h-8 w-36 sm:w-48 rounded-lg border border-slate-800 bg-slate-950 pl-8 pr-2 text-xs text-slate-200 placeholder:text-slate-600 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500"
            />
          </div>

          {/* Autoscroll Toggle */}
          <Button
            size="sm"
            variant={autoScroll ? "default" : "outline"}
            onClick={() => setAutoScroll((v) => !v)}
            title={autoScroll ? "Auto-scroll ON" : "Auto-scroll OFF"}
            className="h-8 px-2.5 text-xs"
          >
            <ArrowDown className="h-3.5 w-3.5 mr-1" />
            Scroll
          </Button>

          {/* Pause / Resume */}
          <Button
            size="sm"
            variant={isPaused ? "warning" : "secondary"}
            onClick={onTogglePause}
            className="h-8 px-2.5 text-xs"
          >
            {isPaused ? (
              <>
                <Play className="h-3.5 w-3.5 mr-1" /> Resume
              </>
            ) : (
              <>
                <Pause className="h-3.5 w-3.5 mr-1" /> Pause
              </>
            )}
          </Button>

          {/* Copy Logs */}
          <Button
            size="sm"
            variant="ghost"
            onClick={handleCopy}
            className="h-8 w-8 p-0 text-slate-400 hover:text-white"
            title="Copy logs"
          >
            {copied ? <Check className="h-3.5 w-3.5 text-emerald-400" /> : <Copy className="h-3.5 w-3.5" />}
          </Button>

          {/* Clear Logs */}
          <Button
            size="sm"
            variant="ghost"
            onClick={onClear}
            className="h-8 w-8 p-0 text-slate-400 hover:text-rose-400"
            title="Clear terminal"
          >
            <Trash2 className="h-3.5 w-3.5" />
          </Button>
        </div>
      </div>

      {/* Terminal Output Area */}
      <div
        ref={scrollContainerRef}
        className="h-[520px] overflow-y-auto overflow-x-hidden p-2 select-text font-mono text-xs bg-[#080d18]"
      >
        {filteredLogs.length === 0 ? (
          <div className="flex h-full flex-col items-center justify-center text-slate-500 space-y-2">
            <TerminalIcon className="h-8 w-8 text-slate-700 animate-pulse" />
            <p className="text-xs">
              {status === "connected"
                ? "Waiting for incoming log stream..."
                : status === "connecting"
                ? "Establishing WebSocket connection..."
                : "No log lines available."}
            </p>
          </div>
        ) : (
          filteredLogs.map((line, idx) => renderLogLine(line, idx))
        )}
      </div>

      {/* Terminal Footer Bar */}
      <div className="flex items-center justify-between border-t border-slate-800/80 bg-slate-900/60 px-4 py-1.5 text-[11px] text-slate-400">
        <div>
          <span>Total Lines: </span>
          <span className="font-semibold text-slate-200">{logs.length}</span>
          {filter && (
            <span className="ml-2 text-sky-400">
              (Showing {filteredLogs.length} matching)
            </span>
          )}
        </div>
        <div className="flex items-center space-x-3">
          {isPaused && (
            <span className="text-amber-400 font-semibold uppercase tracking-wider">
              [PAUSED]
            </span>
          )}
          <span>Buffer: UTF-8 stream</span>
        </div>
      </div>
    </div>
  );
}
