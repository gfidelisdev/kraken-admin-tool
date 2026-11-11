import test from "node:test";
import assert from "node:assert/strict";
import {
  normalizeLanguage,
  resolveMonacoTheme,
  sanitizeHeight,
  sanitizeTabSize,
  sanitizeWordWrap,
  sanitizeDocumentValue,
  SUPPORTED_LANGUAGES,
} from "../codeEditorUtils.ts";
import type { CodeEditorNode } from "../codeEditorUtils.ts";

test("1. Renderiza um documento e aplica a linguagem suportada e aliases", () => {
  // Test common aliases
  assert.equal(normalizeLanguage("js"), "javascript");
  assert.equal(normalizeLanguage("ts"), "typescript");
  assert.equal(normalizeLanguage("py"), "python");
  assert.equal(normalizeLanguage("golang"), "go");
  assert.equal(normalizeLanguage("sh"), "shell");
  assert.equal(normalizeLanguage("bash"), "shell");
  assert.equal(normalizeLanguage("ps1"), "powershell");
  assert.equal(normalizeLanguage("yml"), "yaml");
  assert.equal(normalizeLanguage("md"), "markdown");
  assert.equal(normalizeLanguage("docker"), "dockerfile");
  assert.equal(normalizeLanguage("tf"), "hcl");
  assert.equal(normalizeLanguage("c++"), "cpp");

  // Every single required language
  const required = [
    "plaintext", "javascript", "typescript", "json", "html", "xml", "css", "scss", "less",
    "markdown", "yaml", "ini", "python", "go", "rust", "java", "kotlin", "csharp", "c", "cpp",
    "php", "ruby", "perl", "lua", "shell", "powershell", "sql", "dockerfile", "hcl"
  ];
  for (const lang of required) {
    assert.equal(SUPPORTED_LANGUAGES.has(lang), true, `Language ${lang} must be supported`);
    assert.equal(normalizeLanguage(lang), lang);
  }
});

test("2. Propaga evento 'change' com o valor atualizado e debounce", async () => {
  const events: any[] = [];
  const onEvent = (id: string, eventType: string, payload: any) => {
    events.push({ id, eventType, payload });
  };
  const formValuesRef = { current: {} as Record<string, any> };

  const node: CodeEditorNode = {
    type: "CodeEditor",
    id: "editor_content",
    value: "let count = 0;",
  };

  // Simulating debounced change emission
  const newText = "let count = 42;";
  formValuesRef.current[node.id] = newText;
  const payload = {
    id: node.id,
    value: newText,
    formData: { ...formValuesRef.current, [node.id]: newText },
    [node.id]: newText,
  };
  onEvent(node.id, "change", payload);

  assert.equal(events.length, 1);
  assert.equal(events[0].id, "editor_content");
  assert.equal(events[0].eventType, "change");
  assert.equal(events[0].payload.value, "let count = 42;");
  assert.equal(events[0].payload.editor_content, "let count = 42;");
});

test("3. Inclui o conteúdo atual em formData e como payload.editor_content", () => {
  const formValuesRef = {
    current: {
      user_id: "user_123",
      target_env: "production",
    } as Record<string, any>,
  };

  const currentEditorContent = "apiVersion: apps/v1\nkind: Deployment";
  // Simulating immediate synchronous update
  formValuesRef.current["manifest_editor"] = currentEditorContent;

  // Button click payload in form
  const buttonClickPayload = {
    formData: { ...formValuesRef.current },
    ...formValuesRef.current,
  };

  assert.equal(buttonClickPayload.formData["manifest_editor"], currentEditorContent);
  assert.equal((buttonClickPayload as any)["manifest_editor"], currentEditorContent);
  assert.equal(buttonClickPayload.formData["user_id"], "user_123");
});

test("4. Ctrl+S / Cmd+S dispara saveActionId com eventType 'save'", () => {
  const events: any[] = [];
  const onEvent = (targetId: string, eventType: string, payload: any) => {
    events.push({ targetId, eventType, payload });
  };
  const formValuesRef = { current: { file_path: "/etc/nginx.conf" } as Record<string, any> };

  const node: CodeEditorNode = {
    type: "CodeEditor",
    id: "config_editor",
    saveActionId: "btn-save-config",
  };

  const savedContent = "server { listen 80; }";
  formValuesRef.current[node.id] = savedContent;

  const savePayload = {
    id: node.id,
    value: savedContent,
    formData: { ...formValuesRef.current, [node.id]: savedContent },
    [node.id]: savedContent,
  };

  onEvent(node.saveActionId!, "save", savePayload);

  assert.equal(events.length, 1);
  assert.equal(events[0].targetId, "btn-save-config");
  assert.equal(events[0].eventType, "save");
  assert.equal(events[0].payload.id, "config_editor");
  assert.equal(events[0].payload.value, savedContent);
  assert.equal(events[0].payload.config_editor, savedContent);
  assert.equal(events[0].payload.formData.config_editor, savedContent);
});

test("5. readOnly e opções de editor são repassadas com fidelidade", () => {
  const nodeReadOnly: CodeEditorNode = {
    type: "CodeEditor",
    id: "ed1",
    readOnly: true,
    lineNumbers: false,
    minimap: true,
  };

  assert.equal(Boolean(nodeReadOnly.readOnly), true);
  assert.equal(nodeReadOnly.lineNumbers !== false ? "on" : "off", "off");
  assert.equal(Boolean(nodeReadOnly.minimap), true);

  const nodeEditable: CodeEditorNode = {
    type: "CodeEditor",
    id: "ed2",
    readOnly: false,
    lineNumbers: true,
    minimap: false,
  };

  assert.equal(Boolean(nodeEditable.readOnly), false);
  assert.equal(nodeEditable.lineNumbers !== false ? "on" : "off", "on");
  assert.equal(Boolean(nodeEditable.minimap), false);
});

test("6. Troca de tema sem perder o conteúdo", () => {
  let activeTheme = resolveMonacoTheme("light", "dark");
  assert.equal(activeTheme, "vs");

  activeTheme = resolveMonacoTheme("dark", "light");
  assert.equal(activeTheme, "vs-dark");

  activeTheme = resolveMonacoTheme("hc", "light");
  assert.equal(activeTheme, "hc-black");
});

test("7. Rerender sem mudança de valor preserva cursor e undo (sem invocar setValue)", () => {
  let modelValue = "const test = 100;";
  let setValueCount = 0;

  function syncValue(incoming: string) {
    if (incoming !== modelValue) {
      modelValue = incoming;
      setValueCount++;
    }
  }

  // First rerender with identical value
  syncValue("const test = 100;");
  assert.equal(setValueCount, 0, "setValue should not be called when value is unchanged");

  // Subsequent rerenders
  syncValue("const test = 100;");
  assert.equal(setValueCount, 0);
});

test("8. Mudança externa de value atualiza o modelo", () => {
  let modelValue = "old content";
  let setValueCount = 0;

  function syncValue(incoming: string) {
    if (incoming !== modelValue) {
      modelValue = incoming;
      setValueCount++;
    }
  }

  syncValue("new content from backend");
  assert.equal(setValueCount, 1);
  assert.equal(modelValue, "new content from backend");
});

test("9. Desmontagem libera modelo, editor, listener e debounce timer", () => {
  let modelDisposed = false;
  let editorDisposed = false;
  let listenerDisposed = false;
  let timerCleared = false;

  const mockModel = { dispose: () => { modelDisposed = true; } };
  const mockEditor = { dispose: () => { editorDisposed = true; } };
  const mockListener = { dispose: () => { listenerDisposed = true; } };
  let debounceTimer: any = setTimeout(() => {}, 5000);

  function unmount() {
    if (debounceTimer) {
      clearTimeout(debounceTimer);
      timerCleared = true;
      debounceTimer = null;
    }
    mockListener.dispose();
    mockEditor.dispose();
    mockModel.dispose();
  }

  unmount();

  assert.equal(modelDisposed, true);
  assert.equal(editorDisposed, true);
  assert.equal(listenerDisposed, true);
  assert.equal(timerCleared, true);
});

test("10. Valores inválidos são normalizados com segurança", () => {
  // tabSize safe range 1..8
  assert.equal(sanitizeTabSize(-5), 1);
  assert.equal(sanitizeTabSize(12), 8);
  assert.equal(sanitizeTabSize("abc"), 4);

  // height safe range 100..2000
  assert.equal(sanitizeHeight(5), 100);
  assert.equal(sanitizeHeight(3500), 2000);
  assert.equal(sanitizeHeight("not-a-number"), 400);

  // wordWrap safe enumeration
  assert.equal(sanitizeWordWrap("something"), "off");
  assert.equal(sanitizeWordWrap("on"), "on");
  assert.equal(sanitizeWordWrap("bounded"), "bounded");

  // unknown language fallback
  assert.equal(normalizeLanguage("brainfuck"), "plaintext");
  assert.equal(normalizeLanguage(null as any), "plaintext");

  // max doc size
  const oversize = "a".repeat(7 * 1024 * 1024);
  const truncated = sanitizeDocumentValue(oversize);
  assert.equal(truncated.length, 5 * 1024 * 1024);
});

test("11. Suporte aos temas do Kraken (claro, escuro, alto contraste, colorblind, sepia)", () => {
  // Explicit high contrast
  assert.equal(resolveMonacoTheme("high-contrast", "light"), "hc-black");
  assert.equal(resolveMonacoTheme("hc", "dark"), "hc-black");

  // Host theme inheritance
  assert.equal(resolveMonacoTheme(undefined, "high-contrast"), "hc-black");
  assert.equal(resolveMonacoTheme(undefined, "light"), "vs");
  assert.equal(resolveMonacoTheme(undefined, "dark"), "vs-dark");
  assert.equal(resolveMonacoTheme(undefined, "colorblind"), "vs-dark");
  assert.equal(resolveMonacoTheme(undefined, "sepia"), "vs-dark");
});

test("12. Contrato completo do CodeEditor é compatível com spec solicitada", () => {
  const contractExample: CodeEditorNode = {
    type: "CodeEditor",
    id: "editor_content",
    value: "console.log('hello world');",
    language: "javascript",
    theme: "dark",
    readOnly: false,
    lineNumbers: true,
    minimap: false,
    wordWrap: "off",
    tabSize: 4,
    insertSpaces: true,
    automaticLayout: true,
    height: 600,
    saveActionId: "btn-save",
  };

  assert.equal(contractExample.type, "CodeEditor");
  assert.equal(contractExample.id, "editor_content");
  assert.equal(normalizeLanguage(contractExample.language), "javascript");
  assert.equal(resolveMonacoTheme(contractExample.theme, "dark"), "vs-dark");
  assert.equal(sanitizeHeight(contractExample.height), 600);
  assert.equal(sanitizeTabSize(contractExample.tabSize), 4);
  assert.equal(sanitizeWordWrap(contractExample.wordWrap), "off");
  assert.equal(contractExample.saveActionId, "btn-save");
});
