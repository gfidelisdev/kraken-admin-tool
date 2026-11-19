"use client";

import React, { useEffect, useMemo, useState } from "react";
import { Check, Copy, Loader2, RefreshCw } from "lucide-react";
import { Modal } from "@/components/ui/Modal";
import { Button } from "@/components/ui/Button";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/Tabs";
import type { K8sResourceDescription } from "@/types";

type InspectResult =
  | { kind: "json"; data: unknown }
  | { kind: "k8s"; data: K8sResourceDescription };

interface InspectModalProps {
  isOpen: boolean;
  onClose: () => void;
  title: string;
  description?: string;
  /** Loads the data each time the modal opens or the user refreshes. */
  load: (() => Promise<InspectResult>) | null;
}

function CodeBlock({ text }: { text: string }) {
  const [copied, setCopied] = useState(false);
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(text);
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    } catch {
      /* clipboard unavailable */
    }
  };
  return (
    <div className="relative">
      <button
        onClick={copy}
        className="absolute right-3 top-3 z-10 flex items-center gap-1 rounded-md border border-slate-700 bg-slate-900/90 px-2 py-1 text-xs text-slate-300 hover:text-white"
      >
        {copied ? <Check className="h-3.5 w-3.5 text-emerald-400" /> : <Copy className="h-3.5 w-3.5" />}
        {copied ? "Copied" : "Copy"}
      </button>
      <pre className="max-h-[60vh] overflow-auto rounded-lg border border-slate-800 bg-slate-950 p-4 text-xs leading-relaxed text-slate-200 font-mono whitespace-pre">
        {text || "(empty)"}
      </pre>
    </div>
  );
}

export function InspectModal({ isOpen, onClose, title, description, load }: InspectModalProps) {
  const [result, setResult] = useState<InspectResult | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);
  const [tab, setTab] = useState("describe");
  const [reloadKey, setReloadKey] = useState(0);

  useEffect(() => {
    if (!isOpen || !load) return;
    let cancelled = false;
    setLoading(true);
    setError(null);
    load()
      .then((r) => {
        if (cancelled) return;
        setResult(r);
        if (r.kind === "k8s") setTab(r.data.describe ? "describe" : "manifest");
      })
      .catch((err: unknown) => {
        if (!cancelled) setError(err instanceof Error ? err.message : "Failed to load data");
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [isOpen, load, reloadKey]);

  useEffect(() => {
    if (!isOpen) setResult(null);
  }, [isOpen]);

  const jsonText = useMemo(
    () => (result?.kind === "json" ? JSON.stringify(result.data, null, 2) : ""),
    [result]
  );

  const events = result?.kind === "k8s" ? result.data.events ?? [] : [];

  return (
    <Modal isOpen={isOpen} onClose={onClose} title={title} description={description} maxWidth="6xl">
      <div className="space-y-3">
        <div className="flex items-center justify-end">
          <Button size="sm" variant="secondary" onClick={() => setReloadKey((n) => n + 1)} disabled={loading}>
            <RefreshCw className={`mr-1.5 h-3.5 w-3.5 ${loading ? "animate-spin" : ""}`} />
            Refresh
          </Button>
        </div>

        {loading && !result && (
          <div className="flex items-center justify-center gap-2 py-16 text-sm text-slate-400">
            <Loader2 className="h-4 w-4 animate-spin" /> Loading...
          </div>
        )}

        {error && (
          <div className="rounded-lg border border-rose-500/30 bg-rose-500/10 p-3 text-sm text-rose-300">{error}</div>
        )}

        {result?.kind === "json" && <CodeBlock text={jsonText} />}

        {result?.kind === "k8s" && (
          <Tabs value={tab} onValueChange={setTab}>
            <div className="flex items-center gap-3">
              <TabsList>
                {result.data.describe && <TabsTrigger value="describe">Describe</TabsTrigger>}
                <TabsTrigger value="manifest">{result.data.source === "docker" ? "Inspect" : "Manifest"}</TabsTrigger>
                <TabsTrigger value="events" badge={events.length || undefined}>
                  Events
                </TabsTrigger>
              </TabsList>
              <span className="text-xs text-slate-500">source: {result.data.source}</span>
            </div>
            {result.data.describe && (
              <TabsContent value="describe">
                <CodeBlock text={result.data.describe} />
              </TabsContent>
            )}
            <TabsContent value="manifest">
              <CodeBlock text={result.data.manifest} />
            </TabsContent>
            <TabsContent value="events">
              {events.length === 0 ? (
                <p className="py-8 text-center text-sm text-slate-500">No events found.</p>
              ) : (
                <div className="max-h-[60vh] overflow-auto rounded-lg border border-slate-800">
                  <table className="w-full text-left text-xs">
                    <thead className="sticky top-0 bg-slate-900 text-slate-400">
                      <tr>
                        <th className="px-3 py-2 font-medium">Type</th>
                        <th className="px-3 py-2 font-medium">Reason</th>
                        <th className="px-3 py-2 font-medium">Object</th>
                        <th className="px-3 py-2 font-medium">Message</th>
                        <th className="px-3 py-2 font-medium">Count</th>
                        <th className="px-3 py-2 font-medium">Last seen</th>
                      </tr>
                    </thead>
                    <tbody className="divide-y divide-slate-800 text-slate-300">
                      {events.map((e, i) => (
                        <tr key={i}>
                          <td className={`px-3 py-2 ${e.type === "Warning" ? "text-amber-400" : "text-slate-300"}`}>
                            {e.type}
                          </td>
                          <td className="px-3 py-2">{e.reason}</td>
                          <td className="px-3 py-2 font-mono">{e.object}</td>
                          <td className="px-3 py-2">{e.message}</td>
                          <td className="px-3 py-2">{e.count}</td>
                          <td className="px-3 py-2 whitespace-nowrap">
                            {e.last_seen ? new Date(e.last_seen).toLocaleString() : "-"}
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              )}
            </TabsContent>
          </Tabs>
        )}
      </div>
    </Modal>
  );
}

export type { InspectResult };
