"use client";

import React from "react";
import Link from "next/link";
import { usePathname } from "next/navigation";
import {
  LayoutDashboard,
  Box,
  Layers,
  Server,
  FileText,
  ShieldCheck,
  Disc,
  Anchor,
  Activity,
  Cpu,
  Users,
  Puzzle,
} from "lucide-react";
import { cn } from "@/lib/utils";
import { useAuth } from "@/context/AuthContext";
import { usePlugins } from "@/context/PluginContext";
import Image from "next/image";

const navigation = [
  { name: "Overview", href: "/", icon: LayoutDashboard },
  { name: "Containers", href: "/containers", icon: Box, badge: "Docker" },
  { name: "Stacks", href: "/stacks", icon: Layers, badge: "Compose" },
  { name: "Images", href: "/images", icon: Disc, badge: "Docker" },
  { name: "Hosts", href: "/servers", icon: Server, badge: "Multi" },
  { name: "Pods", href: "/pods", icon: Cpu, badge: "K8s" },
  { name: "Deployments", href: "/deployments", icon: Activity, badge: "K8s" },
  { name: "Live Logs", href: "/logs", icon: FileText, pulse: true },
  { name: "Plugins & Store", href: "/plugins", icon: Puzzle, badge: "Loja" },
  { name: "Audit Trail", href: "/audit", icon: ShieldCheck },
  { name: "Users & Roles", href: "/users", icon: Users, badge: "Admin", adminOnly: true },
];

export function Sidebar({ className }: { className?: string }) {
  const pathname = usePathname();
  const { user } = useAuth();
  const { sidebarPlugins } = usePlugins();

  return (
    <aside
      className={cn(
        "flex flex-col w-64 border-r border-slate-800 bg-[#090d16] select-none shrink-0 h-screen sticky top-0",
        className
      )}
    >
      {/* Brand Header */}
      <div className="flex items-center space-x-3 px-6 h-16 border-b border-slate-800/80 bg-slate-950/40">
        <div className="flex h-10 w-10 items-center justify-center rounded-xl bg-gradient-to-tr from-sky-500 to-indigo-600 shadow-md shadow-sky-500/20 text-white font-bold">
          {/* <Anchor className="h-5 w-5" /> */}
          {/* <span className="h-5 w-5"> */}
          <Image src="/kraken.png" alt="" width={50} height={50} />
          {/* </span> */}
        </div>
        <div>
          <span className="text-base font-bold text-white tracking-tight flex items-center gap-1.5">
            Kraken <span className="text-sky-400 font-medium text-xs">Admin</span>
          </span>
          <p className="text-[10px] text-slate-400 font-mono">Docker & K8s Unified</p>
        </div>
      </div>

      {/* Navigation Links */}
      <nav className="flex-1 space-y-1.5 px-3 py-4 overflow-y-auto">
        <div className="px-3 pb-2 text-[10px] font-semibold uppercase tracking-wider text-slate-500">
          Management
        </div>
        {navigation
          .filter((item) => !item.adminOnly || user?.role === "admin")
          .map((item) => {
            const isActive =
              item.href === "/" ? pathname === "/" : pathname.startsWith(item.href);

            return (
              <Link
                key={item.name}
                href={item.href}
                className={cn(
                  "group flex items-center justify-between rounded-xl px-3 py-2.5 text-xs font-medium transition-all duration-150",
                  isActive
                    ? "bg-slate-800/90 text-white shadow-sm border border-slate-700/60 font-semibold"
                    : "text-slate-400 hover:bg-slate-800/40 hover:text-slate-200"
                )}
              >
                <div className="flex items-center space-x-3">
                  <item.icon
                    className={cn(
                      "h-4 w-4 transition-colors",
                      isActive
                        ? "text-sky-400"
                        : "text-slate-500 group-hover:text-slate-300"
                    )}
                  />
                  <span>{item.name}</span>
                </div>

                {item.badge && (
                  <span
                    className={cn(
                      "text-[10px] font-mono px-1.5 py-0.5 rounded border",
                      item.badge === "Docker"
                        ? "bg-sky-500/10 text-sky-400 border-sky-500/20"
                        : item.badge === "Admin"
                          ? "bg-amber-500/10 text-amber-400 border-amber-500/20"
                          : "bg-indigo-500/10 text-indigo-400 border-indigo-500/20"
                    )}
                  >
                    {item.badge}
                  </span>
                )}

                {item.pulse && (
                  <span className="relative flex h-2 w-2">
                    <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-emerald-400 opacity-75"></span>
                    <span className="relative inline-flex rounded-full h-2 w-2 bg-emerald-500"></span>
                  </span>
                )}
              </Link>
            );
          })}

        {/* Dynamic Active Plugins in Sidebar */}
        {sidebarPlugins.length > 0 && (
          <div className="pt-3 border-t border-slate-800/60 mt-3 space-y-1">
            <div className="px-3 pb-1 text-[10px] font-semibold uppercase tracking-wider text-slate-500">
              Extensões Ativas
            </div>
            {sidebarPlugins.map((item) => {
              const isActive = pathname.startsWith(item.href);
              return (
                <Link
                  key={item.href}
                  href={item.href}
                  className={cn(
                    "group flex items-center justify-between rounded-xl px-3 py-2 text-xs font-medium transition-all duration-150",
                    isActive
                      ? "bg-sky-500/10 text-sky-300 font-semibold border border-sky-500/20"
                      : "text-slate-400 hover:bg-slate-800/40 hover:text-slate-200"
                  )}
                >
                  <div className="flex items-center space-x-3 truncate">
                    {item.plugin.icon_url ? (
                      <img
                        src={item.plugin.icon_url}
                        alt=""
                        className="h-4 w-4 object-contain shrink-0 rounded-sm"
                      />
                    ) : (
                      <Puzzle className="h-3.5 w-3.5 text-sky-400 shrink-0" />
                    )}
                    <span className="truncate">{item.ext.title}</span>
                  </div>
                  {item.ext.badge && (
                    <span className="text-[9px] font-mono px-1 rounded bg-slate-800 text-slate-400 border border-slate-700">
                      {item.ext.badge}
                    </span>
                  )}
                </Link>
              );
            })}
          </div>
        )}
      </nav>

      {/* Cluster / Daemon Quick Status Footer */}
      <div className="p-4 border-t border-slate-800/80 bg-slate-950/30">
        <div className="rounded-xl border border-slate-800/80 bg-slate-900/60 p-3 space-y-2">
          <div className="flex items-center justify-between text-[11px]">
            <span className="text-slate-400 flex items-center gap-1.5">
              <Cpu className="h-3.5 w-3.5 text-sky-400" /> Docker Daemon
            </span>
            <span className="flex items-center gap-1 text-emerald-400 font-mono text-[10px]">
              <span className="h-1.5 w-1.5 rounded-full bg-emerald-400 animate-pulse" />
              Active
            </span>
          </div>

          <div className="flex items-center justify-between text-[11px]">
            <span className="text-slate-400 flex items-center gap-1.5">
              <Activity className="h-3.5 w-3.5 text-indigo-400" /> K8s Cluster
            </span>
            <span className="flex items-center gap-1 text-emerald-400 font-mono text-[10px]">
              <span className="h-1.5 w-1.5 rounded-full bg-emerald-400 animate-pulse" />
              Connected
            </span>
          </div>
        </div>

        <div className="mt-3 flex items-center justify-between px-1 text-[10px] text-slate-500">
          <span>Kraken Tool v1.1.0</span>
          <span className="font-mono">Go + Next.js</span>
        </div>
      </div>
    </aside>
  );
}
