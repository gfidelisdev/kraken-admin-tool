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
     - **Cryptographic Kraken ↔ Sidecar Contract**: Replaces implicit trust in plain HTTP headers (`X-Kraken-User` and `X-Kraken-Role` are eliminated and rejected). All requests carry an HMAC-SHA256 signature (`X-Kraken-Signature: v1=...`) computed over canonical request string `KRKN1`, body SHA-256 hash, and a short-lived (30s) internal context JWT (`X-Kraken-User-Context`).
     - **Anti-Replay & Session Binding**: Enforces non-repeatable nonces and request IDs with a 60-second retention cache and 30-second timestamp window (returning HTTP 409 `REPLAY_DETECTED` on duplicates). User actions are bound via `kraken_session_id = base64url(SHA256(pepper + session_id))`.
     - **Instance Secrets & Graceful Rotation**: Each installation receives a 32-byte cryptographically random secret (`KRAKEN_PLUGIN_INSTANCE_SECRET`), supporting graceful secret rotation with a 60-second grace period for the previous key.
     - **Lifecycle Callbacks**: Lifecycle events (`POST /v1/internal/session-events`) notify sidecars on session termination (`kraken.session.ended`), user deactivation (`kraken.user.disabled`), and plugin revocation (`plugin.installation.revoked`).

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
