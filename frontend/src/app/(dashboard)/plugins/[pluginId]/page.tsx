"use client";

import React, { use } from "react";
import { PluginSurfaceRenderer } from "@/components/plugins/PluginSurfaceRenderer";
import { Button } from "@/components/ui/Button";
import { ArrowLeft, Puzzle } from "lucide-react";
import Link from "next/link";
import { usePlugins } from "@/context/PluginContext";

interface PluginPageProps {
  params: Promise<{
    pluginId: string;
  }>;
}

export default function PluginHostPage({ params }: PluginPageProps) {
  const resolvedParams = use(params);
  const pluginId = decodeURIComponent(resolvedParams.pluginId);
  const { plugins } = usePlugins();
  const activePlugin = plugins.find((p) => p.plugin_id === pluginId);

  return (
    <div className="space-y-6">
      {/* Top Breadcrumb & Return Button */}
      <div className="flex items-center justify-between pb-4 border-b border-slate-800">
        <div className="flex items-center gap-3">
          <Link href="/plugins">
            <Button variant="outline" size="sm" className="h-8 px-2.5 text-xs text-slate-400 hover:text-white">
              <ArrowLeft className="h-3.5 w-3.5 mr-1" />
              Plugins
            </Button>
          </Link>
          <div className="h-4 w-[1px] bg-slate-800" />
          <div className="flex items-center gap-2 text-xs font-mono text-slate-400">
            {activePlugin?.icon_url ? (
              <img
                src={activePlugin.icon_url}
                alt=""
                className="h-4 w-4 object-contain rounded-sm"
              />
            ) : (
              <Puzzle className="h-4 w-4 text-sky-400" />
            )}
            <span>Plugin: <strong className="text-white">{activePlugin?.name || pluginId}</strong></span>
          </div>
        </div>
      </div>

      {/* Sandboxed Surface View */}
      <PluginSurfaceRenderer pluginId={pluginId} surface="page" />
    </div>
  );
}
