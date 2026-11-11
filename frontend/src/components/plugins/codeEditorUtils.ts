export interface CodeEditorNode {
  type: "CodeEditor";
  id: string;
  value?: string;
  defaultValue?: string;
  language?: string;
  theme?: "dark" | "light" | "hc" | string;
  readOnly?: boolean;
  lineNumbers?: boolean;
  minimap?: boolean;
  wordWrap?: "off" | "on" | "bounded" | string;
  tabSize?: number;
  insertSpaces?: boolean;
  automaticLayout?: boolean;
  height?: number;
  saveActionId?: string;
  [key: string]: any;
}

export const SUPPORTED_LANGUAGES = new Set([
  "plaintext",
  "javascript",
  "typescript",
  "json",
  "html",
  "xml",
  "css",
  "scss",
  "less",
  "markdown",
  "yaml",
  "ini",
  "python",
  "go",
  "rust",
  "java",
  "kotlin",
  "csharp",
  "c",
  "cpp",
  "php",
  "ruby",
  "perl",
  "lua",
  "shell",
  "powershell",
  "sql",
  "dockerfile",
  "hcl",
]);

export const LANGUAGE_ALIASES: Record<string, string> = {
  js: "javascript",
  jsx: "javascript",
  ts: "typescript",
  tsx: "typescript",
  py: "python",
  py3: "python",
  golang: "go",
  sh: "shell",
  bash: "shell",
  zsh: "shell",
  ps1: "powershell",
  pwsh: "powershell",
  yml: "yaml",
  md: "markdown",
  cs: "csharp",
  "c#": "csharp",
  "c++": "cpp",
  cplusplus: "cpp",
  txt: "plaintext",
  text: "plaintext",
  docker: "dockerfile",
  dockerfile: "dockerfile",
  tf: "hcl",
  terraform: "hcl",
  hcl: "hcl",
  json: "json",
  html: "html",
  htm: "html",
  xml: "xml",
  css: "css",
  scss: "scss",
  less: "less",
  markdown: "markdown",
  yaml: "yaml",
  ini: "ini",
  python: "python",
  go: "go",
  rust: "rust",
  rs: "rust",
  java: "java",
  kotlin: "kotlin",
  kt: "kotlin",
  csharp: "csharp",
  c: "c",
  cpp: "cpp",
  php: "php",
  ruby: "ruby",
  rb: "ruby",
  perl: "perl",
  pl: "perl",
  lua: "lua",
  shell: "shell",
  powershell: "powershell",
  sql: "sql",
  plaintext: "plaintext",
};

export function normalizeLanguage(lang?: string): string {
  if (!lang || typeof lang !== "string") return "plaintext";
  const cleaned = lang.trim().toLowerCase();
  const alias = LANGUAGE_ALIASES[cleaned];
  if (alias && SUPPORTED_LANGUAGES.has(alias)) {
    return alias;
  }
  if (SUPPORTED_LANGUAGES.has(cleaned)) {
    return cleaned;
  }
  return "plaintext";
}

export function resolveMonacoTheme(nodeTheme?: string, appTheme?: string): "vs" | "vs-dark" | "hc-black" {
  const t = (nodeTheme || "").trim().toLowerCase();
  if (t === "light" || t === "vs") return "vs";
  if (t === "hc" || t === "high-contrast" || t === "hc-black") return "hc-black";
  if (t === "dark" || t === "vs-dark") return "vs-dark";

  const app = (appTheme || "").trim().toLowerCase();
  if (app === "light") return "vs";
  if (app === "high-contrast") return "hc-black";
  return "vs-dark";
}

export function sanitizeHeight(height?: any): number {
  const num = typeof height === "number" ? height : parseInt(String(height), 10);
  if (isNaN(num)) return 400;
  return Math.min(Math.max(num, 100), 2000);
}

export function sanitizeTabSize(tabSize?: any): number {
  const num = typeof tabSize === "number" ? tabSize : parseInt(String(tabSize), 10);
  if (isNaN(num)) return 4;
  return Math.min(Math.max(num, 1), 8);
}

export function sanitizeWordWrap(wrap?: string): "off" | "on" | "bounded" {
  if (wrap === "on" || wrap === "bounded" || wrap === "off") return wrap;
  return "off";
}

// 5MB text document safety limit
export const MAX_DOC_CHARS = 5 * 1024 * 1024;

export function sanitizeDocumentValue(val: any): string {
  if (val === null || val === undefined) return "";
  const str = String(val);
  if (str.length > MAX_DOC_CHARS) {
    console.warn(`[CodeEditor] Document exceeded maximum allowed size (5MB). Truncating safely.`);
    return str.slice(0, MAX_DOC_CHARS);
  }
  return str;
}
