# 0003: Plugin Architecture, Isolation Without IFrames, and Runtime Sandboxing

## Context
Kraken Admin Tool requires an extensible plugin ecosystem enabling third parties and organizations to add custom administrative capabilities, UI surfaces, and background workflows.
Because plugins can originate from untrusted external sources, security isolation is paramount. In traditional web administration dashboards, iframes are frequently used for third-party UI widgets. However, iframes present severe limitations:
1. They create visual boundaries, scrollbars, and lack seamless theme synchronization (Dark, Light, High Contrast, Colorblind Safe, Sepia).
2. They introduce complex communication overhead and storage security issues.
3. They fail to prevent arbitrary token theft if not strictly sandboxed, or break desired interactions if overly constrained.

On the backend, plugins must execute with strict bounds to prevent unauthorized local filesystem access, arbitrary host command execution, or lateral privilege escalation.

## Decisions

1. **Frontend Isolation Without IFrames (Web Worker Sandbox + Remote UI + Closed Shadow DOM)**:
   - Plugin JavaScript code executes exclusively inside a dedicated, isolated `Worker` thread (`PluginSurfaceRenderer`).
   - The Web Worker environment has **zero access** to the host DOM (`window`, `document`), no access to `localStorage` (where `kraken_auth_token` resides), and no access to cookies or navigation history.
   - **Remote UI Declarative Protocol**: The worker computes state and transmits declarative JSON UI trees (`Page`, `Section`, `Grid`, `StatCard`, `Card`, `Table`, `Button`, `Badge`, `Alert`, `Terminal`, `CustomShadowHtml`) over `postMessage`.
   - The host React application renders the declarative tree using native Kraken UI components, automatically and seamlessly inheriting active CSS variables and theme styles.
   - For custom styling or rich HTML/SVG visualizations, the renderer utilizes a closed `ShadowRoot` with sanitized tags (via DOMPurify or tag allowlisting), ensuring visual style encapsulation without iframes.

2. **Backend Dual-Runtime Model**:
   - **WASM Sandbox (Default)**: Embedded, pure-Go WebAssembly runtime via `wazero` (zero CGO dependencies). Executes compiled WASM modules (`GOOS=wasip1 GOARCH=wasm`, Rust, C) with configurable memory ceilings (default 64MB) and execution timeouts (15s). Host functions provide strictly mediated logging (`kraken_log`) and scoped key-value storage (`kraken_kv_get`, `kraken_kv_set`).
   - **Container Sidecar (Advanced)**: Dedicated isolated Docker container per plugin, managed through the host Docker daemon with isolated bridge networks, read-only root filesystems, and strict CPU/memory quotas.

3. **Archive Packaging & Safe Extraction**:
   - Plugins are packaged as self-contained `.zip` or `.tar.gz` archives containing `plugin.json` (manifest), `README.md`, optional frontend entrypoint (`frontend/worker.js`), and optional backend binary (`backend/plugin.wasm`).
   - The extractor enforces:
     - Strict Zip-Slip prevention (`filepath.Rel` and lexical path validation).
     - Decompression bomb protection (max 50MB archive size, max 150MB extracted size, max 500 files).
     - Rejection of symlinks, hardlinks, sockets, and character devices.

4. **Cryptographic Signatures & Ed25519 Trust Store**:
   - Plugin archives may be signed using standard Ed25519 digital signatures.
   - Administrators manage trusted public keys in the Kraken database (`PluginTrustedKey`).
   - During inspection and installation, the system verifies whether the archive matches a trusted public key and displays the verified publisher badge in the UI.

5. **Online Plugin Store & Catalog Aggregation**:
   - Kraken supports subscribing to multiple remote or private catalogs via JSON index files (`index.json`).
   - Administrators can install directly from store listings, paste an archive download URL, or upload local archives with one click.
   - Permissions declared in `plugin.json` must be explicitly reviewed and acknowledged by the administrator prior to installation.

## Status
Accepted
