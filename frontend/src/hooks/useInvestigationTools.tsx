"use client";

import React, { useCallback, useState } from "react";
import { TerminalModal, TerminalTarget } from "@/components/modals/TerminalModal";
import { InspectModal, InspectResult } from "@/components/modals/InspectModal";

interface InspectState {
  title: string;
  description?: string;
  load: () => Promise<InspectResult>;
}

/**
 * Shared state for the admin-only Terminal (exec) and Inspect/Describe modals,
 * so list pages only need to call openTerminal/openInspect and render `modals`.
 */
export function useInvestigationTools(serverId?: number) {
  const [terminal, setTerminal] = useState<TerminalTarget | null>(null);
  const [inspect, setInspect] = useState<InspectState | null>(null);

  const openTerminal = useCallback((target: TerminalTarget) => setTerminal(target), []);
  const openInspect = useCallback((state: InspectState) => setInspect(state), []);
  const closeTerminal = useCallback(() => setTerminal(null), []);
  const closeInspect = useCallback(() => setInspect(null), []);

  const modals = (
    <>
      <TerminalModal isOpen={!!terminal} onClose={closeTerminal} target={terminal} serverId={serverId} />
      <InspectModal
        isOpen={!!inspect}
        onClose={closeInspect}
        title={inspect?.title ?? "Inspect"}
        description={inspect?.description}
        load={inspect?.load ?? null}
      />
    </>
  );

  return { openTerminal, openInspect, modals };
}
