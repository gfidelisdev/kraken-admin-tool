"use client";

import React from "react";
import { Modal } from "@/components/ui/Modal";
import { Button } from "@/components/ui/Button";
import { Badge } from "@/components/ui/Badge";
import {
  Eye,
  EyeOff,
  Maximize2,
  Minimize2,
  ArrowUp,
  ArrowDown,
  RotateCcw,
  SlidersHorizontal,
  Puzzle,
} from "lucide-react";

export interface DashboardItemDefinition {
  id: string;
  title: string;
  description: string;
  icon: React.ReactNode;
}

export interface CustomizeDashboardModalProps {
  isOpen: boolean;
  onClose: () => void;
  definitions: DashboardItemDefinition[];
  order: string[];
  visibility: Record<string, boolean>;
  collapsed: Record<string, boolean>;
  onReorder: (newOrder: string[]) => void;
  onToggleVisibility: (id: string) => void;
  onToggleCollapse: (id: string) => void;
  onSetAllVisibility: (visible: boolean) => void;
  onSetAllCollapse: (collapsed: boolean) => void;
  onReset: () => void;
  // Independent Plugin Widgets customization
  pluginWidgets?: DashboardItemDefinition[];
  widgetOrder?: string[];
  widgetVisibility?: Record<string, boolean>;
  widgetCollapsed?: Record<string, boolean>;
  onReorderWidgets?: (newOrder: string[]) => void;
  onToggleWidgetVisibility?: (id: string) => void;
  onToggleWidgetCollapse?: (id: string) => void;
  onSetAllWidgetVisibility?: (visible: boolean) => void;
}

export function CustomizeDashboardModal({
  isOpen,
  onClose,
  definitions,
  order,
  visibility,
  collapsed,
  onReorder,
  onToggleVisibility,
  onToggleCollapse,
  onSetAllVisibility,
  onSetAllCollapse,
  onReset,
  pluginWidgets = [],
  widgetOrder = [],
  widgetVisibility = {},
  widgetCollapsed = {},
  onReorderWidgets,
  onToggleWidgetVisibility,
  onToggleWidgetCollapse,
  onSetAllWidgetVisibility,
}: CustomizeDashboardModalProps) {
  const defMap = React.useMemo(() => {
    const map = new Map<string, DashboardItemDefinition>();
    definitions.forEach((d) => map.set(d.id, d));
    return map;
  }, [definitions]);

  const widgetDefMap = React.useMemo(() => {
    const map = new Map<string, DashboardItemDefinition>();
    pluginWidgets.forEach((d) => map.set(d.id, d));
    return map;
  }, [pluginWidgets]);

  const handleMove = (index: number, direction: "up" | "down") => {
    const targetIndex = direction === "up" ? index - 1 : index + 1;
    if (targetIndex < 0 || targetIndex >= order.length) return;

    const newOrder = [...order];
    const [moved] = newOrder.splice(index, 1);
    newOrder.splice(targetIndex, 0, moved);
    onReorder(newOrder);
  };

  const handleMoveWidget = (index: number, direction: "up" | "down") => {
    if (!onReorderWidgets) return;
    const targetIndex = direction === "up" ? index - 1 : index + 1;
    if (targetIndex < 0 || targetIndex >= widgetOrder.length) return;

    const newOrder = [...widgetOrder];
    const [moved] = newOrder.splice(index, 1);
    newOrder.splice(targetIndex, 0, moved);
    onReorderWidgets(newOrder);
  };

  const visibleCount = order.filter((id) => visibility[id] !== false).length;
  const effectiveWidgetOrder = widgetOrder.filter((id) => widgetDefMap.has(id));
  const visibleWidgetCount = effectiveWidgetOrder.filter(
    (id) => widgetVisibility[id] !== false
  ).length;

  return (
    <Modal
      isOpen={isOpen}
      onClose={onClose}
      title="Personalizar Dashboard"
      description="Organize a ordem dos blocos e dos widgets de plugins, alterne entre visualização expandida ou compacta e escolha quais itens exibir independentemente na página inicial."
      maxWidth="xl"
    >
      <div className="space-y-6 max-h-[72vh] overflow-y-auto pr-1">
        {/* Section 1: Main Dashboard Sections */}
        <div className="space-y-3">
          <div className="flex flex-wrap items-center justify-between gap-2 p-3 rounded-lg bg-slate-900/80 border border-slate-800 text-xs">
            <div className="flex items-center gap-2 text-slate-300">
              <SlidersHorizontal className="h-4 w-4 text-sky-400" />
              <span>
                Blocos da Dashboard: <strong className="text-white">{visibleCount}</strong> de {order.length} visíveis
              </span>
            </div>
            <div className="flex items-center gap-1.5 flex-wrap">
              <Button
                variant="outline"
                size="sm"
                onClick={() => onSetAllVisibility(true)}
                className="text-[11px] h-7 px-2"
              >
                Exibir Todos
              </Button>
              <Button
                variant="outline"
                size="sm"
                onClick={() => onSetAllCollapse(false)}
                className="text-[11px] h-7 px-2"
              >
                Expandir Todos
              </Button>
              <Button
                variant="outline"
                size="sm"
                onClick={() => onSetAllCollapse(true)}
                className="text-[11px] h-7 px-2"
              >
                Compactar Todos
              </Button>
            </div>
          </div>

          <div className="space-y-2">
            {order.map((id, index) => {
              const def = defMap.get(id);
              if (!def) return null;

              const isVisible = visibility[id] !== false;
              const isCollapsed = Boolean(collapsed[id]);

              return (
                <div
                  key={id}
                  className={`flex items-center justify-between p-3.5 rounded-xl border transition-all ${
                    isVisible
                      ? "bg-slate-900/70 border-slate-800 text-slate-200 hover:border-slate-700"
                      : "bg-slate-950/40 border-slate-900/80 text-slate-500 opacity-60"
                  }`}
                >
                  <div className="flex items-center gap-3 min-w-0 pr-3">
                    <span className="text-xs font-mono font-bold text-slate-500 w-5 text-center">
                      #{index + 1}
                    </span>
                    <div
                      className={`p-2 rounded-lg shrink-0 ${
                        isVisible ? "bg-slate-800 text-sky-400" : "bg-slate-900 text-slate-600"
                      }`}
                    >
                      {def.icon}
                    </div>
                    <div className="min-w-0">
                      <div className="flex items-center gap-2">
                        <p className="text-xs font-semibold text-white truncate">{def.title}</p>
                        {isVisible && (
                          <Badge
                            variant={isCollapsed ? "outline" : "running"}
                            className="text-[9px] px-1.5 py-0 uppercase"
                          >
                            {isCollapsed ? "Compacto" : "Expandido"}
                          </Badge>
                        )}
                      </div>
                      <p className="text-[11px] text-slate-400 truncate mt-0.5">{def.description}</p>
                    </div>
                  </div>

                  <div className="flex items-center gap-1.5 shrink-0">
                    <Button
                      variant="ghost"
                      size="sm"
                      onClick={() => handleMove(index, "up")}
                      disabled={index === 0}
                      title="Mover para cima"
                      className="h-8 w-8 p-0 text-slate-400 hover:text-white disabled:opacity-30"
                    >
                      <ArrowUp className="h-4 w-4" />
                    </Button>

                    <Button
                      variant="ghost"
                      size="sm"
                      onClick={() => handleMove(index, "down")}
                      disabled={index === order.length - 1}
                      title="Mover para baixo"
                      className="h-8 w-8 p-0 text-slate-400 hover:text-white disabled:opacity-30"
                    >
                      <ArrowDown className="h-4 w-4" />
                    </Button>

                    <Button
                      variant="ghost"
                      size="sm"
                      onClick={() => onToggleCollapse(id)}
                      title={isCollapsed ? "Mudar para Expandido" : "Mudar para Compacto"}
                      disabled={!isVisible}
                      className="h-8 w-8 p-0 text-slate-400 hover:text-sky-400 disabled:opacity-30"
                    >
                      {isCollapsed ? (
                        <Maximize2 className="h-4 w-4" />
                      ) : (
                        <Minimize2 className="h-4 w-4" />
                      )}
                    </Button>

                    <Button
                      variant={isVisible ? "ghost" : "secondary"}
                      size="sm"
                      onClick={() => onToggleVisibility(id)}
                      title={isVisible ? "Ocultar da dashboard" : "Exibir na dashboard"}
                      className={`h-8 w-8 p-0 ${
                        isVisible
                          ? "text-slate-400 hover:text-rose-400"
                          : "text-emerald-400 hover:text-emerald-300"
                      }`}
                    >
                      {isVisible ? <Eye className="h-4 w-4" /> : <EyeOff className="h-4 w-4" />}
                    </Button>
                  </div>
                </div>
              );
            })}
          </div>
        </div>

        {/* Section 2: Independent Plugin Widgets */}
        {effectiveWidgetOrder.length > 0 && (
          <div className="space-y-3 pt-2 border-t border-slate-800/80">
            <div className="flex flex-wrap items-center justify-between gap-2 p-3 rounded-lg bg-slate-900/80 border border-slate-800 text-xs">
              <div className="flex items-center gap-2 text-slate-300">
                <Puzzle className="h-4 w-4 text-sky-400" />
                <span>
                  Widgets de Plugins: <strong className="text-white">{visibleWidgetCount}</strong> de{" "}
                  {effectiveWidgetOrder.length} visíveis (reordene e escolha quais exibir)
                </span>
              </div>
              {onSetAllWidgetVisibility && (
                <div className="flex items-center gap-1.5 flex-wrap">
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => onSetAllWidgetVisibility(true)}
                    className="text-[11px] h-7 px-2"
                  >
                    Exibir Todos os Widgets
                  </Button>
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => onSetAllWidgetVisibility(false)}
                    className="text-[11px] h-7 px-2"
                  >
                    Ocultar Todos
                  </Button>
                </div>
              )}
            </div>

            <div className="space-y-2">
              {effectiveWidgetOrder.map((wId, idx) => {
                const wDef = widgetDefMap.get(wId);
                if (!wDef) return null;

                const isWVisible = widgetVisibility[wId] !== false;
                const isWCollapsed = Boolean(widgetCollapsed[wId]);

                return (
                  <div
                    key={wId}
                    className={`flex items-center justify-between p-3.5 rounded-xl border transition-all ${
                      isWVisible
                        ? "bg-slate-900/70 border-slate-800 text-slate-200 hover:border-slate-700"
                        : "bg-slate-950/40 border-slate-900/80 text-slate-500 opacity-60"
                    }`}
                  >
                    <div className="flex items-center gap-3 min-w-0 pr-3">
                      <span className="text-xs font-mono font-bold text-slate-500 w-5 text-center">
                        #{idx + 1}
                      </span>
                      <div
                        className={`p-2 rounded-lg shrink-0 ${
                          isWVisible ? "bg-slate-800 text-sky-400" : "bg-slate-900 text-slate-600"
                        }`}
                      >
                        {wDef.icon}
                      </div>
                      <div className="min-w-0">
                        <div className="flex items-center gap-2">
                          <p className="text-xs font-semibold text-white truncate">{wDef.title}</p>
                          <Badge
                            variant={isWVisible ? (isWCollapsed ? "outline" : "running") : "stopped"}
                            className="text-[9px] px-1.5 py-0 uppercase"
                          >
                            {!isWVisible ? "Oculto" : isWCollapsed ? "Compacto" : "Visível"}
                          </Badge>
                        </div>
                        <p className="text-[11px] text-slate-400 truncate mt-0.5">{wDef.description}</p>
                      </div>
                    </div>

                    <div className="flex items-center gap-1.5 shrink-0">
                      <Button
                        variant="ghost"
                        size="sm"
                        onClick={() => handleMoveWidget(idx, "up")}
                        disabled={idx === 0}
                        title="Mover widget para cima"
                        className="h-8 w-8 p-0 text-slate-400 hover:text-white disabled:opacity-30"
                      >
                        <ArrowUp className="h-4 w-4" />
                      </Button>

                      <Button
                        variant="ghost"
                        size="sm"
                        onClick={() => handleMoveWidget(idx, "down")}
                        disabled={idx === effectiveWidgetOrder.length - 1}
                        title="Mover widget para baixo"
                        className="h-8 w-8 p-0 text-slate-400 hover:text-white disabled:opacity-30"
                      >
                        <ArrowDown className="h-4 w-4" />
                      </Button>

                      {onToggleWidgetCollapse && (
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() => onToggleWidgetCollapse(wId)}
                          title={isWCollapsed ? "Expandir widget" : "Compactar widget"}
                          disabled={!isWVisible}
                          className="h-8 w-8 p-0 text-slate-400 hover:text-sky-400 disabled:opacity-30"
                        >
                          {isWCollapsed ? (
                            <Maximize2 className="h-4 w-4" />
                          ) : (
                            <Minimize2 className="h-4 w-4" />
                          )}
                        </Button>
                      )}

                      {onToggleWidgetVisibility && (
                        <Button
                          variant={isWVisible ? "ghost" : "secondary"}
                          size="sm"
                          onClick={() => onToggleWidgetVisibility(wId)}
                          title={isWVisible ? "Ocultar este widget" : "Exibir este widget"}
                          className={`h-8 w-8 p-0 ${
                            isWVisible
                              ? "text-slate-400 hover:text-rose-400"
                              : "text-emerald-400 hover:text-emerald-300"
                          }`}
                        >
                          {isWVisible ? <Eye className="h-4 w-4" /> : <EyeOff className="h-4 w-4" />}
                        </Button>
                      )}
                    </div>
                  </div>
                );
              })}
            </div>
          </div>
        )}

        {/* Footer */}
        <div className="flex items-center justify-between pt-4 border-t border-slate-800">
          <Button
            variant="outline"
            size="sm"
            onClick={onReset}
            className="text-xs text-slate-400 hover:text-slate-200"
          >
            <RotateCcw className="mr-1.5 h-3.5 w-3.5" />
            Restaurar Padrão
          </Button>

          <Button variant="default" size="sm" onClick={onClose} className="text-xs">
            Concluir
          </Button>
        </div>
      </div>
    </Modal>
  );
}
