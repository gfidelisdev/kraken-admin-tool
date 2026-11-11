# Stage 5: Initial Zoom Out & Inventive Catalyst Review Report

* **Target File**: `design_draft.md` (Full File, Lines 1–381)
* **Reviewers**: `egm-zoom-out` (Skeptical Architect), `egm-inventive-catalyst` (TRIZ Inventive Catalyst)
* **Standard**: `zoom-out-standard.md`

---

## 1. Zoom Out Review (`egm-zoom-out`)

Overall, the design has clear context sufficiency, directly addresses the root causes (duplicate login flows, global `PluginKVStore` credential bleed across users, and multi-resource audience token brokering without exposing refresh tokens to Web Workers), and reuses Kraken's existing JWT, RBAC (`pending` approval workflow), and Wazero host module boundaries.

### Finding ZO-1: Redundant & Boundary-Violating Host Adapter Operation (`auth.get_session_token`)
* **Severity**: Major
* **Affected Scope**: `## Goals` (line 31), `### 3. Plugin Context & Host Bridge Integration` (line 132), and `#### 9. backend/internal/plugins/adapter.go` (lines 278–283)
* **Simplicity Rationale**:
  The design proposes two separate interfaces for plugins to request a scoped Microsoft token:
  * Synchronous WASM host import `kraken_auth_get_token` in `backend/internal/plugins/wasm_runtime.go`.
  * Host adapter operation `"auth.get_session_token"` in `backend/internal/plugins/adapter.go` (`ResolveHostAdapter`).

  First, no component or plugin in the plan uses `"auth.get_session_token"` (Phase 4 exclusively uses `kraken_auth_get_token`), making Step 9 a YAGNI violation. Second, in Kraken's plugin architecture (`CallPlugin` in `manager.go`), `ResolveHostAdapter` intercepts the final stdout of a WASM invocation (`status: "needs_host_adapter"`) and returns the result directly to the HTTP caller (the browser/Web Worker) rather than returning control to the WASM binary. Exposing a raw `access_token` through `ResolveHostAdapter` would violate the design's security invariant (lines 128 and 162) that raw OAuth2 tokens must never be returned to the browser or Web Worker.
  * **Recommendation**: Remove Step 9 (`backend/internal/plugins/adapter.go`) and all references to `auth.get_session_token` (lines 31 and 132), keeping `kraken_auth_get_token` on the WASM host bridge as the single token-brokering interface.

### Finding ZO-2: No-Op Account Filtering Proposed in `syncPluginServerBindings`
* **Severity**: Nit
* **Affected Scope**: `#### 8. backend/internal/plugins/manager.go` (line 276)
* **Simplicity Rationale**:
  Line 276 proposes updating `syncPluginServerBindings(pluginID, configJSON)` to ignore any account entry with `Source == "kraken_session"`. Inspection of `syncPluginServerBindings` shows that it only unmarshals `ServerBindings map[string]string` (`json:"server_bindings"`) and never inspects, unmarshals, or persists `accounts`. Furthermore, `worker.js` already strips `source === "kraken_session"` in `persistConfig()` before writing `azure_kv_config`.
  * **Recommendation**: Remove the `syncPluginServerBindings` bullet from Step 8 to avoid adding dead unmarshaling/filtering logic to `manager.go`.

### Finding ZO-3: Speculative `auth:microsoft` Permission Identifier
* **Severity**: Nit
* **Affected Scope**: `### 3. Plugin Context & Host Bridge Integration` (line 131), `## Non-Functional Requirements` (line 163), and `#### 7. backend/internal/plugins/wasm_runtime.go` (line 265)
* **Simplicity Rationale**:
  The architecture diagram (line 99) states that `kraken_auth_get_token` verifies the plugin has `'credentials:resolve'` or `'credentials:manage'`, both of which are existing permissions already declared in `com.kraken.azure-keyvault/plugin.json`. Lines 131, 163, and 265 add a third unreferenced permission string (`bridge.Permissions["auth:microsoft"]`) that no plugin manifest requests and no inspector defines.
  * **Recommendation**: Gate `kraken_auth_get_token` strictly on the existing `credentials:resolve` and `credentials:manage` permissions (matching line 99) and drop the speculative `auth:microsoft` permission check.

---

## 2. Inventive Catalyst Review (`egm-inventive-catalyst`)

* **Result**: No false compromise detected. The session-scoped Backend Token Broker paired with on-demand WASM host bridge scope exchange resolves the contradiction between OAuth2 v2.0 single-audience tokens and Web Worker / global KV isolation without compromise.
