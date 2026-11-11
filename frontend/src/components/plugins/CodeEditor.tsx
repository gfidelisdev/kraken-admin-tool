"use client";

import React, { useEffect, useRef, useState } from "react";
import { RefreshCw } from "lucide-react";
import { useTheme } from "@/context/ThemeContext";
import {
  CodeEditorNode,
  normalizeLanguage,
  resolveMonacoTheme,
  sanitizeHeight,
  sanitizeTabSize,
  sanitizeWordWrap,
  sanitizeDocumentValue,
} from "./codeEditorUtils";

export type { CodeEditorNode };
export {
  normalizeLanguage,
  resolveMonacoTheme,
  sanitizeHeight,
  sanitizeTabSize,
  sanitizeWordWrap,
  sanitizeDocumentValue,
};

interface CodeEditorProps {
  node: CodeEditorNode;
  fieldVal: any;
  updateFormField: (id: string, val: any) => void;
  onEvent: (id: string, type: string, payload?: any) => void;
  formValuesRef: React.MutableRefObject<Record<string, any>>;
}

export function CodeEditor({
  node,
  fieldVal,
  updateFormField,
  onEvent,
  formValuesRef,
}: CodeEditorProps) {
  const containerRef = useRef<HTMLDivElement>(null);
  const editorRef = useRef<any>(null);
  const modelRef = useRef<any>(null);
  const monacoModuleRef = useRef<any>(null);
  const debounceTimerRef = useRef<NodeJS.Timeout | null>(null);
  const [isMonacoReady, setIsMonacoReady] = useState(false);

  const { theme: hostTheme } = useTheme();

  const safeHeight = sanitizeHeight(node.height);
  const safeTabSize = sanitizeTabSize(node.tabSize);
  const safeWordWrap = sanitizeWordWrap(node.wordWrap);
  const safeLanguage = normalizeLanguage(node.language);
  const targetTheme = resolveMonacoTheme(node.theme, hostTheme);

  const propsRef = useRef({
    node,
    fieldVal,
    updateFormField,
    onEvent,
    formValuesRef,
    targetTheme,
    safeLanguage,
    safeWordWrap,
    safeTabSize,
  });

  useEffect(() => {
    propsRef.current = {
      node,
      fieldVal,
      updateFormField,
      onEvent,
      formValuesRef,
      targetTheme,
      safeLanguage,
      safeWordWrap,
      safeTabSize,
    };
  });

  // Initialize Monaco environment and dynamically load Monaco Editor
  useEffect(() => {
    let isCancelled = false;

    if (typeof window !== "undefined" && !(window as any).MonacoEnvironment) {
      (window as any).MonacoEnvironment = {
        getWorker: function (_moduleId: any, label: string) {
          if (label === "json") {
            return new Worker(new URL("monaco-editor/language/json/json.worker.js", import.meta.url));
          }
          if (label === "css" || label === "scss" || label === "less") {
            return new Worker(new URL("monaco-editor/language/css/css.worker.js", import.meta.url));
          }
          if (label === "html" || label === "handlebars" || label === "razor") {
            return new Worker(new URL("monaco-editor/language/html/html.worker.js", import.meta.url));
          }
          if (label === "typescript" || label === "javascript") {
            return new Worker(new URL("monaco-editor/language/typescript/ts.worker.js", import.meta.url));
          }
          return new Worker(new URL("monaco-editor/editor/editor.worker.js", import.meta.url));
        },
      };
    }

    import("monaco-editor")
      .then((monaco) => {
        if (isCancelled) return;
        monacoModuleRef.current = monaco;
        setIsMonacoReady(true);
      })
      .catch((err) => {
        console.error("[CodeEditor] Failed to load Monaco Editor:", err);
      });

    return () => {
      isCancelled = true;
    };
  }, []);

  // Instantiate editor when container and Monaco are both ready
  useEffect(() => {
    if (!isMonacoReady || !containerRef.current || editorRef.current) return;
    const monaco = monacoModuleRef.current;
    if (!monaco) return;

    const {
      node: currentNode,
      fieldVal: currentFieldVal,
      updateFormField: currentUpdateFormField,
      onEvent: currentOnEvent,
      formValuesRef: currentFormValuesRef,
      targetTheme: currentTargetTheme,
      safeLanguage: currentSafeLanguage,
      safeWordWrap: currentSafeWordWrap,
      safeTabSize: currentSafeTabSize,
    } = propsRef.current;

    const initialContent = sanitizeDocumentValue(
      currentFieldVal !== undefined ? currentFieldVal : (currentNode.value ?? currentNode.defaultValue ?? "")
    );

    // Synchronously ensure field is registered in formValues
    if (currentNode.id) {
      currentFormValuesRef.current[currentNode.id] = initialContent;
    }

    const model = monaco.editor.createModel(initialContent, currentSafeLanguage);
    modelRef.current = model;

    const editor = monaco.editor.create(containerRef.current, {
      model: model,
      theme: currentTargetTheme,
      readOnly: Boolean(currentNode.readOnly),
      lineNumbers: currentNode.lineNumbers !== false ? "on" : "off",
      minimap: { enabled: Boolean(currentNode.minimap) },
      wordWrap: currentSafeWordWrap,
      tabSize: currentSafeTabSize,
      insertSpaces: currentNode.insertSpaces !== false,
      automaticLayout: currentNode.automaticLayout !== false,
      bracketPairColorization: { enabled: true },
      scrollBeyondLastLine: false,
      roundedSelection: true,
      fontSize: 13,
      fontFamily: "var(--font-mono, 'Fira Code', 'JetBrains Mono', Menlo, Monaco, Consolas, monospace)",
      overviewRulerLanes: 0,
      hideCursorInOverviewRuler: true,
    });
    editorRef.current = editor;

    // Listen to changes
    const changeListener = model.onDidChangeContent(() => {
      const currentVal = editor.getValue();
      const latestProps = propsRef.current;
      if (latestProps.node.id) {
        // Immediately sync form field value so button clicks get the latest content synchronously
        latestProps.updateFormField(latestProps.node.id, currentVal);

        // Debounce the worker "change" event
        if (debounceTimerRef.current) {
          clearTimeout(debounceTimerRef.current);
        }
        debounceTimerRef.current = setTimeout(() => {
          latestProps.onEvent(latestProps.node.id, "change", {
            id: latestProps.node.id,
            value: currentVal,
            formData: { ...latestProps.formValuesRef.current, [latestProps.node.id]: currentVal },
            [latestProps.node.id]: currentVal,
          });
        }, 250);
      }
    });

    // Intercept Ctrl+S / Cmd+S for saveActionId
    editor.addCommand(monaco.KeyMod.CtrlCmd | monaco.KeyCode.KeyS, () => {
      const latestProps = propsRef.current;
      if (latestProps.node.saveActionId) {
        if (debounceTimerRef.current) {
          clearTimeout(debounceTimerRef.current);
          debounceTimerRef.current = null;
        }
        const currentVal = editor.getValue();
        if (latestProps.node.id) {
          latestProps.updateFormField(latestProps.node.id, currentVal);
        }
        latestProps.onEvent(latestProps.node.saveActionId, "save", {
          id: latestProps.node.id,
          value: currentVal,
          formData: { ...latestProps.formValuesRef.current, [latestProps.node.id]: currentVal },
          [latestProps.node.id]: currentVal,
        });
      }
    });

    return () => {
      if (debounceTimerRef.current) {
        clearTimeout(debounceTimerRef.current);
        debounceTimerRef.current = null;
      }
      changeListener.dispose();
      editor.dispose();
      editorRef.current = null;
      model.dispose();
      modelRef.current = null;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [isMonacoReady]);

  // Sync external value changes without disturbing cursor / undo when content is identical
  useEffect(() => {
    if (!editorRef.current) return;
    const currentVal = editorRef.current.getValue();
    const incomingVal = sanitizeDocumentValue(
      fieldVal !== undefined ? fieldVal : (node.value ?? node.defaultValue ?? "")
    );

    if (incomingVal !== currentVal) {
      editorRef.current.setValue(incomingVal);
    }
  }, [fieldVal, node.value, node.defaultValue]);

  // Sync auxiliary options without recreating the editor
  useEffect(() => {
    if (!editorRef.current) return;
    editorRef.current.updateOptions({
      readOnly: Boolean(node.readOnly),
      lineNumbers: node.lineNumbers !== false ? "on" : "off",
      minimap: { enabled: Boolean(node.minimap) },
      wordWrap: safeWordWrap,
      tabSize: safeTabSize,
      insertSpaces: node.insertSpaces !== false,
      automaticLayout: node.automaticLayout !== false,
    });
  }, [node.readOnly, node.lineNumbers, node.minimap, safeWordWrap, safeTabSize, node.insertSpaces, node.automaticLayout]);

  // Sync theme changes
  useEffect(() => {
    if (!monacoModuleRef.current || !editorRef.current) return;
    monacoModuleRef.current.editor.setTheme(targetTheme);
  }, [targetTheme]);

  // Sync language changes
  useEffect(() => {
    if (!monacoModuleRef.current || !modelRef.current) return;
    monacoModuleRef.current.editor.setModelLanguage(modelRef.current, safeLanguage);
  }, [safeLanguage]);

  // Intercept Ctrl+S / Cmd+S at the container level as an extra safeguard
  const handleKeyDown = (e: React.KeyboardEvent) => {
    if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === "s") {
      e.preventDefault();
      e.stopPropagation();
      if (node.saveActionId && editorRef.current) {
        if (debounceTimerRef.current) {
          clearTimeout(debounceTimerRef.current);
          debounceTimerRef.current = null;
        }
        const currentVal = editorRef.current.getValue();
        if (node.id) {
          updateFormField(node.id, currentVal);
        }
        onEvent(node.saveActionId, "save", {
          id: node.id,
          value: currentVal,
          formData: { ...formValuesRef.current, [node.id]: currentVal },
          [node.id]: currentVal,
        });
      }
    }
  };

  return (
    <div
      className="space-y-1.5 w-full"
      onKeyDown={handleKeyDown}
      style={{ minHeight: `${safeHeight}px` }}
    >
      {node.label && (
        <label className="text-xs font-medium text-slate-300 block">
          {node.label}
          {node.required && <span className="text-rose-400 ml-1">*</span>}
        </label>
      )}

      <div
        className="relative w-full rounded-xl overflow-hidden border border-slate-800 bg-slate-950 shadow-inner focus-within:ring-1 focus-within:ring-sky-500/70"
        style={{ height: `${safeHeight}px` }}
      >
        {!isMonacoReady && (
          <div
            className="absolute inset-0 flex flex-col items-center justify-center bg-slate-950 text-slate-400 space-y-2 select-none"
            style={{ height: `${safeHeight}px` }}
          >
            <RefreshCw className="h-5 w-5 animate-spin text-sky-400" />
            <span className="text-xs font-mono">Carregando editor de código...</span>
          </div>
        )}

        <div
          ref={containerRef}
          className="w-full h-full"
          style={{ height: `${safeHeight}px` }}
        />
      </div>

      {(node.description || node.helperText) && (
        <p className="text-[11px] text-slate-400">{node.description || node.helperText}</p>
      )}
    </div>
  );
}
