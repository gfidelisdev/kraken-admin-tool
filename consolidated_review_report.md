# Consolidated Goldfish Review Report (`consolidated_review_report.md`)

**Target Document**: [`design_draft.md`](file:///home/gfidelis/Code/go/kraken-admin-tool/design_draft.md)
**Semantic Split Boundaries**:
- `FeatureRange`: Lines `1..171` (`# Microsoft Entra ID Authentication & Plugin Session Token Broker` through `### Verification Strategy`)
- `DetailedRange`: Lines `172..381` (`## Detailed Design` through `### Verification Plan`)

**Stage Reports**:
- Stage 5 (Zoom-Out & Inventive Catalyst): [`zoom_out_report.md`](file:///home/gfidelis/Code/go/kraken-admin-tool/zoom_out_report.md)
- Stage 6 (Comprehension): [`comprehension_report.md`](file:///home/gfidelis/Code/go/kraken-admin-tool/comprehension_report.md)
- Stage 7 (Design, Structural & Style Critic): [`critic_report.md`](file:///home/gfidelis/Code/go/kraken-admin-tool/critic_report.md)
- Stage 8 (Intermediate Zoom-Out): [`intermediate_zoom_out_report.md`](file:///home/gfidelis/Code/go/kraken-admin-tool/intermediate_zoom_out_report.md)
- Stage 9 (Implementation Readiness): [`implementation_readiness_report.md`](file:///home/gfidelis/Code/go/kraken-admin-tool/implementation_readiness_report.md)

---

## Executive Summary

Across all five review stages (8 independent Goldfish reviewer personas), **21 raw findings** were deduplicated into **16 consolidated action items**:
- **🔴 Blockers (4)**: Codebase and protocol contradictions that would cause broken runtime behavior, `401 Unauthorized` responses from Microsoft Graph, silent fallback to the wrong Azure account in `com.kraken.azure-keyvault`, or untestable OAuth2 endpoints.
- **🟡 Majors (7)**: Architectural simplification (`adapter.go` dead surface removal), RFC 8628 `slow_down` compliance, per-session locking during token refresh, `Non-Functional Requirements` risk coverage, `auth.NewHandler` test signature compatibility, `UserResponse` metadata population on login, and Phase 2/4 file boundary allowlist completeness.
- **🟢 Nits (5)**: Removal of speculative `auth:microsoft` permission constant, `azure-kv-integration` script generation context, `detectKrakenUser()` vault registry restoration order, `PluginSurfaceRenderer.tsx` `useEffect` note, and mechanical style cleanup.

---

## 🔴 Blockers

### CR-1: Broken Active Account Selection & Token Resolution in `azure-kv-integration` When `kraken_session` Is Stripped from Persistence
* **Severity**: 🔴 Blocker
* **Originating Findings**: `IR-1` (`egm-implementation-readiness` #1, `egm-codebase-details` #2)
* **Affected Lines**: [`design_draft.md`](file:///home/gfidelis/Code/go/kraken-admin-tool/design_draft.md#L135), [`design_draft.md`](file:///home/gfidelis/Code/go/kraken-admin-tool/design_draft.md#L340-L360)
* **Codebase Reference**: [`/home/gfidelis/Code/kraken-plugins/azure-kv-integration/backend/main.go`](file:///home/gfidelis/Code/kraken-plugins/azure-kv-integration/backend/main.go#L151-L319)
* **Problem**:
  `persistConfig()` in `frontend/worker.js` strips `source === "kraken_session"` accounts before writing `azure_kv_config` to `PluginKVStore`. In `backend/main.go`, `mergeWithPersistedConfig(incoming *Config)` ignores `incoming.Accounts` and retains only `persisted.Accounts` from `PluginKVStore`, and `getActiveAccount(cfg)` falls back to `&cfg.Accounts[0]` whenever `cfg.ActiveAccountID` is not found in `cfg.Accounts`. If a standalone `device_login` account was previously persisted, `getActiveAccount(cfg)` returns `&cfg.Accounts[0]` (`Source == "device_login"`), causing `if acc == nil || acc.Source == "kraken_session"` in Item 15 to evaluate to `false` and silently use the standalone account instead of the active Kraken Microsoft session.
* **Required Revision**:
  1. In Phase 4 Item 15 (`backend/main.go`), define `findExplicitStandaloneAccount(cfg *Config) *MicrosoftAccount` that returns a persisted account from `cfg.Accounts` only when `cfg.ActiveAccountID != ""` and `cfg.ActiveAccountID != "kraken-ms-session"` and `cfg.Accounts[i].ID == cfg.ActiveAccountID` with `cfg.Accounts[i].Source != "kraken_session"`.
  2. Under `case "user_account":` in `buildAuthTokenScript` and `buildARMTokenScript`, if `findExplicitStandaloneAccount(cfg)` is `nil`, call `wasmGetMicrosoftSessionToken(...)` first (`"https://vault.azure.net/.default"` or `"https://management.azure.com/.default"`). If non-empty, emit `AKV_TOKEN=<shellQuotedToken>` / `ARM_TOKEN=<shellQuotedToken>`.
  3. Only if `wasmGetMicrosoftSessionToken(...)` returns `""` (local Kraken session) or an explicit standalone account was selected should `buildAuthTokenScript` and `buildARMTokenScript` fall back to `getActiveAccount(cfg)` and refresh token exchange.
  4. Standardize the helper function name to `wasmGetMicrosoftSessionToken` across line 135 and line 345.

---

### CR-2: OIDC `userinfo` Audience Mismatch & Unspecified `msTokenPayload` Struct Definition
* **Severity**: 🔴 Blocker
* **Originating Findings**: `C-1` (`egm-comprehension` #1), `IR-2` (`egm-implementation-readiness` #2, `egm-codebase-details` #3)
* **Affected Lines**: [`design_draft.md`](file:///home/gfidelis/Code/go/kraken-admin-tool/design_draft.md#L112-L120), [`design_draft.md`](file:///home/gfidelis/Code/go/kraken-admin-tool/design_draft.md#L221-L228)
* **Codebase Reference**: [`/home/gfidelis/Code/kraken-plugins/azure-kv-integration/backend/main.go`](file:///home/gfidelis/Code/kraken-plugins/azure-kv-integration/backend/main.go#L412-L448)
* **Problem**:
  1. Requesting `scope=openid profile email offline_access https://vault.azure.net/.default` from Entra ID v2.0 yields an `access_token` with audience `https://vault.azure.net`. Calling `GET https://graph.microsoft.com/oidc/userinfo` with that token fails with `401 Unauthorized (Invalid Audience)`.
  2. Line 227 states *"via `GET https://graph.microsoft.com/oidc/userinfo` (or token claims `preferred_username`, `email`, `upn`, `name`, `tid`)"*, leaving an implementation choice (`or`) and omitting the `msTokenPayload` struct definition.
* **Required Revision**:
  1. Define `msTokenPayload` in Phase 1 Item 4 (`backend/internal/auth/ms_broker.go`) with `AccessToken string`, `RefreshToken string`, `IDToken string`, `ExpiresIn int`, `Error string`, and `ErrorDesc string`.
  2. Remove the `GET https://graph.microsoft.com/oidc/userinfo` reference and specify deterministic base64url JWT payload decoding of `tokenPayload.IDToken` (merging claims from `tokenPayload.AccessToken` if `IDToken` is empty or missing claims):
     - `email`: first non-empty claim containing `"@"` among `email`, `preferred_username`, `upn`, `unique_name` (error if none contains `"@"`).
     - `name`: first non-empty claim among `name`, `given_name`, falling back to the email local-part before `"@"`.
     - `tenantID`: `tid` claim, falling back to `fallbackTenant`.
     - `id`: `oid` or `sub` claim, falling back to `"kraken-ms-" + email`.
  3. Clarify in `FeatureRange` (`### Microsoft Device Code Login & Account Provisioning`) that Entra ID validates the user's credentials when issuing the token response over TLS to `https://login.microsoftonline.com`, allowing the backend to decode identity claims directly from the returned token payload.

---

### CR-3: Missing Configurable `EntraBaseURL` on `MicrosoftSessionBroker` for `httptest.NewServer`
* **Severity**: 🔴 Blocker
* **Originating Findings**: `IR-3` (`egm-implementation-readiness` #2, `egm-codebase-details` #4)
* **Affected Lines**: [`design_draft.md`](file:///home/gfidelis/Code/go/kraken-admin-tool/design_draft.md#L195-L228), [`design_draft.md`](file:///home/gfidelis/Code/go/kraken-admin-tool/design_draft.md#L245-L254)
* **Problem**:
  Phase 1 Item 3 & Item 4 hardcode `https://login.microsoftonline.com/{tenant}/oauth2/v2.0/...`, preventing `backend/internal/auth/ms_broker_test.go` (Item 6) from redirecting device-code, token-polling, and scoped-token refresh requests to a local `httptest.NewServer`.
* **Required Revision**:
  Add `EntraBaseURL string` (defaulting to `"https://login.microsoftonline.com"`) and `HTTPClient *http.Client` (defaulting to `&http.Client{Timeout: 15 * time.Second}`) to `MicrosoftSessionBroker` in `backend/internal/auth/ms_broker.go`.

---

### CR-4: Misplaced `syncPluginServerBindings` Account Filtering Instruction in `backend/internal/plugins/manager.go`
* **Severity**: 🔴 Blocker
* **Originating Findings**: `ZO-2` (`egm-zoom-out` #2), `IZO-2` (Stage 8), `IR-4` (`egm-implementation-readiness` #4, `egm-codebase-details` #1)
* **Affected Lines**: [`design_draft.md`](file:///home/gfidelis/Code/go/kraken-admin-tool/design_draft.md#L276)
* **Codebase Reference**: [`backend/internal/plugins/manager.go`](file:///home/gfidelis/Code/go/kraken-admin-tool/backend/internal/plugins/manager.go#L1316-L1371)
* **Problem**:
  Phase 2 Item 8 (line 276) instructs the implementer to filter `Source == "kraken_session"` inside `syncPluginServerBindings(pluginID, rawConfigJSON)`. In `manager.go`, `syncPluginServerBindings` only unmarshals `ServerBindings map[string]string` to update `models.DockerServer` rows and never inspects `accounts`.
* **Required Revision**:
  Remove the `syncPluginServerBindings` account-filtering bullet from Phase 2 Item 8 (`backend/internal/plugins/manager.go`), keeping account sanitization in `frontend/worker.js` `persistConfig()` (Phase 4 Item 16).

---

## 🟡 Majors

### CR-5: Remove Unused `ResolveHostAdapter` (`"auth.get_session_token"`) Endpoint from `adapter.go`
* **Severity**: 🟡 Major
* **Originating Findings**: `ZO-1` (`egm-zoom-out` #1), `IZO-1` (Stage 8)
* **Affected Lines**: [`design_draft.md`](file:///home/gfidelis/Code/go/kraken-admin-tool/design_draft.md#L31), [`design_draft.md`](file:///home/gfidelis/Code/go/kraken-admin-tool/design_draft.md#L132), [`design_draft.md`](file:///home/gfidelis/Code/go/kraken-admin-tool/design_draft.md#L278-L286)
* **Codebase Reference**: [`backend/internal/plugins/adapter.go`](file:///home/gfidelis/Code/go/kraken-admin-tool/backend/internal/plugins/adapter.go#L16-L157)
* **Problem**:
  Phase 2 Item 9 adds `"auth.get_session_token"` to `ResolveHostAdapter` in `adapter.go`. However, Phase 4 (`azure-kv-integration/backend/main.go`) exclusively uses the WASM host import `kraken_auth_get_token` (`wasm_runtime.go`), and `ResolveHostAdapter` is reachable via HTTP `POST /api/plugins/:id/adapters/resolve`, which would expose raw Microsoft access tokens to the browser if called.
* **Required Revision**:
  Remove Phase 2 Item 9 (`backend/internal/plugins/adapter.go`) and remove the `"auth.get_session_token"` references on lines 31 and 132 so token delegation occurs strictly across the internal WASM host boundary (`kraken_auth_get_token`).

---

### CR-6: RFC 8628 `slow_down` Handling & Polling Rate-Limit Separation
* **Severity**: 🟡 Major
* **Originating Findings**: `D-1` (`egm-design` #1)
* **Affected Lines**: [`design_draft.md`](file:///home/gfidelis/Code/go/kraken-admin-tool/design_draft.md#L114), [`design_draft.md`](file:///home/gfidelis/Code/go/kraken-admin-tool/design_draft.md#L219), [`design_draft.md`](file:///home/gfidelis/Code/go/kraken-admin-tool/design_draft.md#L292), [`design_draft.md`](file:///home/gfidelis/Code/go/kraken-admin-tool/design_draft.md#L315)
* **Codebase Reference**: [`backend/cmd/server/main.go`](file:///home/gfidelis/Code/go/kraken-admin-tool/backend/cmd/server/main.go#L142-L148)
* **Problem**:
  1. Line 219 maps both `"authorization_pending"` and `"slow_down"` from Entra ID to `MicrosoftPollResponse{Status: "pending_authorization"}`. Per RFC 8628 §3.5, `"slow_down"` requires the client to increase its polling interval by 5 seconds; masking it causes Entra ID to revoke the `device_code`.
  2. Applying the 1-req/2s `authRateLimiter` (burst 5) in `backend/cmd/server/main.go` to `POST /api/auth/microsoft/device-poll` can return `429 Too Many Requests` during normal polling + UI retries.
* **Required Revision**:
  1. Return `Status: "slow_down"` (or include `Interval int` in `MicrosoftPollResponse`) when Entra ID returns `"slow_down"`, and increment the frontend polling interval in `Login.tsx` by `+5000ms` when `"slow_down"` is received.
  2. Apply `authRateLimiter` to `POST /api/auth/microsoft/device-code`, and either exempt `POST /api/auth/microsoft/device-poll` from the strict 0.5 req/s limiter or use a polling-compatible rate limit.

---

### CR-7: Per-Session Concurrency & Expired Session Sweeping in `MicrosoftSessionBroker`
* **Severity**: 🟡 Major
* **Originating Findings**: `D-2` (`egm-design` #2)
* **Affected Lines**: [`design_draft.md`](file:///home/gfidelis/Code/go/kraken-admin-tool/design_draft.md#L101), [`design_draft.md`](file:///home/gfidelis/Code/go/kraken-admin-tool/design_draft.md#L197-L209)
* **Problem**:
  1. Holding a global `sync.RWMutex` write lock on `MicrosoftSessionBroker` while making an outbound 15-second HTTP `POST` to `https://login.microsoftonline.com/{tenant}/oauth2/v2.0/token` in `GetScopedToken` blocks all other sessions and `/api/auth/me` requests.
  2. Expired sessions are only evicted lazily when `GetScopedToken` or `GetAccountMetadata` is called for that specific `sessionID`; abandoned sessions that never call those methods again remain in memory.
* **Required Revision**:
  1. Release the global broker map lock before executing the outbound HTTP token refresh call in `GetScopedToken` (using a per-session `mu sync.Mutex` on `msBrokeredSession` to prevent duplicate concurrent refreshes for the same session, then re-acquiring the lock to store the refreshed token).
  2. Sweep and delete all entries where `time.Now().After(sess.ExpiresAt)` inside `CreateSession`.

---

### CR-8: Add `Risks & Limits` to `Non-Functional Requirements` (Restart Demotion, Tenant Pinning & First-User Bootstrap)
* **Severity**: 🟡 Major
* **Originating Findings**: `D-3` (`egm-design` #3), `D-4` (`egm-design` #4), `S-1` (`egm-structural` #1)
* **Affected Lines**: [`design_draft.md`](file:///home/gfidelis/Code/go/kraken-admin-tool/design_draft.md#L56-L62), [`design_draft.md`](file:///home/gfidelis/Code/go/kraken-admin-tool/design_draft.md#L116-L119)
* **Problem**:
  `## Non-Functional Requirements` in `FeatureRange` omits a `Risks & Limits` item covering:
  1. Ephemeral in-memory `MicrosoftSessionBroker` state loss on backend container restart (where the 24h JWT remains valid, `/api/auth/me` downgrades `auth_provider` to `"local"`, and plugins fall back to standalone login or re-authentication).
  2. Multi-tenant `MICROSOFT_TENANT_ID=common` security considerations (requiring `Status = "pending"` admin approval and recommending pinning `MICROSOFT_TENANT_ID` to a specific Entra Tenant UUID in enterprise deployments).
  3. Fresh-database bootstrap (`userCount == 0`) behavior when the first user signs in via Microsoft vs. local registration.
* **Required Revision**:
  Add a `**Risks & Limits**:` bullet under `## Non-Functional Requirements` documenting these three operational boundaries and mitigations.

---

### CR-9: Preserve 3-Argument `auth.NewHandler` Signature for Unit Test Compatibility
* **Severity**: 🟡 Major
* **Originating Findings**: `IR-5` (`egm-implementation-readiness` #4, `egm-codebase-details` #5)
* **Affected Lines**: [`design_draft.md`](file:///home/gfidelis/Code/go/kraken-admin-tool/design_draft.md#L229-L233), [`design_draft.md`](file:///home/gfidelis/Code/go/kraken-admin-tool/design_draft.md#L288-L294)
* **Codebase Reference**: [`backend/internal/auth/handler_test.go`](file:///home/gfidelis/Code/go/kraken-admin-tool/backend/internal/auth/handler_test.go#L37)
* **Problem**:
  `backend/internal/auth/handler_test.go` calls `auth.NewHandler(db, "test-secret", false)` at 8 call sites. Changing `NewHandler`'s parameter list would break existing tests.
* **Required Revision**:
  Specify in Phase 1 Item 5 and Phase 2 Item 10 that `NewHandler(db *gorm.DB, secret string, allowRegistration bool) *Handler` keeps its 3-argument signature (initializing `MSBroker: NewMicrosoftSessionBroker()`, `MicrosoftClientID: DefaultMicrosoftClientID`, `MicrosoftTenantID: DefaultMicrosoftTenantID`) and adds `SetMicrosoftConfig(clientID, tenantID string)` called from `backend/cmd/server/main.go`.

---

### CR-10: Populate `UserResponse` Microsoft Metadata & Align Pending Role in `PollMicrosoftDeviceLogin`
* **Severity**: 🟡 Major
* **Originating Findings**: `IR-6` (`egm-codebase-details` #6)
* **Affected Lines**: [`design_draft.md`](file:///home/gfidelis/Code/go/kraken-admin-tool/design_draft.md#L222-L225)
* **Codebase Reference**: [`backend/internal/auth/handler.go`](file:///home/gfidelis/Code/go/kraken-admin-tool/backend/internal/auth/handler.go#L97-L115), [`backend/internal/auth/handler.go`](file:///home/gfidelis/Code/go/kraken-admin-tool/backend/internal/auth/handler.go#L211-L220)
* **Problem**:
  1. `toUserResponse(user)` only populates `ID`, `Email`, `Name`, `Role`, `Status`, and `CreatedAt`. Unless `PollMicrosoftDeviceLogin` explicitly sets `userResp.AuthProvider = "microsoft"` and `userResp.MicrosoftAccount = h.MSBroker.GetAccountMetadata(sessionID)` on `userResp` before returning `MicrosoftPollResponse`, the initial login response will lack `auth_provider` and `microsoft_account`.
  2. Line 223 assigns `Role: "viewer"`, `Status: "pending"` to new unrecognized users, whereas `Handler.Register` in `handler.go` uses `Role: "pending"`, `Status: "pending"`.
* **Required Revision**:
  Specify in Phase 1 Item 4 (`PollMicrosoftDeviceLogin`) that `userResp.AuthProvider = "microsoft"` and `userResp.MicrosoftAccount = h.MSBroker.GetAccountMetadata(sessionID)` are set on `userResp`, and align new pending user creation (`Role: "pending"`, `Status: "pending"`) with `Handler.Register`.

---

### CR-11: Include `wasm_runtime_test.go` and `backend/plugin.wasm` in Phase 2 & Phase 4 Target File Lists
* **Severity**: 🟡 Major
* **Originating Findings**: `IR-7` (`egm-implementation-readiness` #3, `egm-codebase-details` #7)
* **Affected Lines**: [`design_draft.md`](file:///home/gfidelis/Code/go/kraken-admin-tool/design_draft.md#L257-L295), [`design_draft.md`](file:///home/gfidelis/Code/go/kraken-admin-tool/design_draft.md#L338-L373)
* **Problem**:
  Verification Plan line 373 requires a WASM host bridge token retrieval test in `./internal/plugins/...`, and Phase 4 recompiles `/home/gfidelis/Code/kraken-plugins/azure-kv-integration/backend/plugin.wasm`, but neither `backend/internal/plugins/wasm_runtime_test.go (Modify)` nor `/home/gfidelis/Code/kraken-plugins/azure-kv-integration/backend/plugin.wasm (Modify)` appears as a numbered target file header in Phase 2 / Phase 4.
* **Required Revision**:
  Add `backend/internal/plugins/wasm_runtime_test.go (Modify)` as a numbered target file in Phase 2 and `/home/gfidelis/Code/kraken-plugins/azure-kv-integration/backend/plugin.wasm (Modify)` as a numbered target file in Phase 4.

---

## 🟢 Nits

### CR-12: Drop Unnecessary `PermissionAuthMicrosoft` (`auth:microsoft`) Constant
* **Severity**: 🟢 Nit
* **Originating Findings**: `ZO-3` (`egm-zoom-out` #3), `IZO-3` (Stage 8), `IR-8` (Stage 9)
* **Affected Lines**: [`design_draft.md`](file:///home/gfidelis/Code/go/kraken-admin-tool/design_draft.md#L261), [`design_draft.md`](file:///home/gfidelis/Code/go/kraken-admin-tool/design_draft.md#L268), [`design_draft.md`](file:///home/gfidelis/Code/go/kraken-admin-tool/design_draft.md#L275)
* **Required Revision**:
  Remove Phase 2 Item 7 (`backend/internal/plugins/manifest.go`) and check `m.hasPermission(pluginID, PermissionCredentialsResolve) || m.hasPermission(pluginID, PermissionCredentialsManage)` in `OnGetSessionToken`.

---

### CR-13: Clarify `azure-kv-integration` Ephemeral Bash Script Execution Model in `FeatureRange`
* **Severity**: 🟢 Nit
* **Originating Findings**: `C-2` (`egm-comprehension` #2)
* **Affected Lines**: [`design_draft.md`](file:///home/gfidelis/Code/go/kraken-admin-tool/design_draft.md#L21), [`design_draft.md`](file:///home/gfidelis/Code/go/kraken-admin-tool/design_draft.md#L135)
* **Required Revision**:
  Briefly explain on line 21 / line 135 that `com.kraken.azure-keyvault` executes Azure REST calls by generating short-lived `curl` scripts executed on the Kraken host via `kraken_host_exec`, and that injecting `AKV_TOKEN` / `ARM_TOKEN` from `wasmGetMicrosoftSessionToken` avoids embedding `refresh_token` in the generated script.

---

### CR-14: Restore Vault Registry in `detectKrakenUser()` in `frontend/worker.js`
* **Severity**: 🟢 Nit
* **Originating Findings**: `IR-10` (`egm-implementation-readiness` #5)
* **Affected Lines**: [`design_draft.md`](file:///home/gfidelis/Code/go/kraken-admin-tool/design_draft.md#L352-L360)
* **Required Revision**:
  Specify in Phase 4 Item 16 that after `detectKrakenUser()` injects the `"kraken-ms-session"` account, if `state.config.vault_name` is empty, it restores `vault_name`, `vault_url`, and `discoveredVaults` from `state.accountVaultRegistry[emailKey]` (or `state.accountVaultRegistry["__last_configured__"]`).

---

### CR-15: Note `PluginSurfaceRenderer.tsx` `useEffect` Dependency Array Behavior
* **Severity**: 🟢 Nit
* **Originating Findings**: `IR-9` (`egm-codebase-details` #8)
* **Affected Lines**: [`design_draft.md`](file:///home/gfidelis/Code/go/kraken-admin-tool/design_draft.md#L332-L336)
* **Required Revision**:
  Note in Phase 3 Item 14 (`frontend/src/components/PluginSurfaceRenderer.tsx`) that `const { user } = useAuth()` is passed in the worker `"init"` payload on mount without adding `user` to the worker-recreation `useEffect` dependency list (`[plugin.id, plugin.frontend_bundle_url, surface]`).

---

### CR-16: Mechanical Style & Terminology Cleanup Across `FeatureRange` and `DetailedRange`
* **Severity**: 🟢 Nit
* **Originating Findings**: `C-3` (`egm-comprehension` #3), `S-2` (`egm-structural` #2), `ST-1`..`ST-6` (`egm-style` #1–#6), `IR-11` (Stage 9)
* **Affected Lines**: Lines `15`, `27`, `117`, `158`, `159`, `224`, `329` of [`design_draft.md`](file:///home/gfidelis/Code/go/kraken-admin-tool/design_draft.md)
* **Required Revision**:
  - Line 15: Replace *"adds first-class Microsoft authentication"* with *"adds Microsoft authentication"*.
  - Line 27: Define `jti` on first use as *"the Kraken JWT's `jti` (JWT ID, a unique session identifier)"*.
  - Line 117: Replace *"logs in cleanly without overwriting"* with *"logs in without overwriting"*.
  - Line 158: Replace *"rejected by the user during design clarification"* with *"rejected to enforce session-scoped credential lifetimes"*.
  - Line 159: Replace *"ensuring proper user lifecycle governance"* with *"enforcing administrator approval before account activation"*.
  - Line 224: Replace *"logs in cleanly without modifying"* with *"logs in without modifying"*.
  - Line 329: Replace *"Receives the full `User` object"* with *"Receives the `User` object"*.

---

## Traceability Matrix (Raw Stage Findings → Consolidated Action Items)

| Stage | Raw Finding ID | Persona | Severity | Consolidated ID | Summary |
| :--- | :--- | :--- | :--- | :--- | :--- |
| Stage 5 | `ZO-1` | `egm-zoom-out` | 🟡 Major | `CR-5` | Remove unused `"auth.get_session_token"` adapter in `adapter.go` |
| Stage 5 | `ZO-2` | `egm-zoom-out` | 🟡 Major | `CR-4` | Remove `syncPluginServerBindings` account-filtering instruction |
| Stage 5 | `ZO-3` | `egm-zoom-out` | 🟢 Nit | `CR-12` | Drop speculative `auth:microsoft` permission constant |
| Stage 5 | `IC-1` | `egm-inventive-catalyst` | — (Validation) | — | Confirmed TRIZ Separation in Space (`MicrosoftSessionBroker` + `kraken_auth_get_token`) |
| Stage 6 | `C-1` | `egm-comprehension` | 🟡 Major | `CR-2` | Clarify OIDC token claim validation & remove `userinfo` audience contradiction |
| Stage 6 | `C-2` | `egm-comprehension` | 🟢 Nit | `CR-13` | Explain `azure-kv-integration` ephemeral bash script execution model |
| Stage 6 | `C-3` | `egm-comprehension` | 🟢 Nit | `CR-16` | Define `jti` (JWT ID) on first use |
| Stage 7 | `D-1` | `egm-design` | 🟡 Major | `CR-6` | Handle RFC 8628 `slow_down` interval backoff & polling rate limit |
| Stage 7 | `D-2` | `egm-design` | 🟡 Major | `CR-7` | Release global broker lock during HTTP refresh & sweep expired sessions |
| Stage 7 | `D-3` | `egm-design` | 🟡 Major | `CR-8` | Document single-instance restart session demotion in `Risks & Limits` |
| Stage 7 | `D-4` | `egm-design` | 🟡 Major | `CR-8` | Document `MICROSOFT_TENANT_ID` tenant-pinning & first-user bootstrap in `Risks & Limits` |
| Stage 7 | `S-1` | `egm-structural` | 🟡 Major | `CR-8` | Add `Risks & Limits` bullet to `Non-Functional Requirements` |
| Stage 7 | `S-2` | `egm-structural` | 🟢 Nit | `CR-16` | Replace unlinked stakeholder attribution in Alternative C |
| Stage 7 | `ST-1`..`ST-4` | `egm-style` | 🟢 Nit | `CR-16` | Strip promotional/filler words in `FeatureRange` (`first-class`, `cleanly`, `proper`) |
| Stage 8 | `IZO-1`..`IZO-3` | `egm-zoom-out` | 🟡 Major / 🟢 Nit | `CR-4`, `CR-5`, `CR-12` | Intermediate architectural alignment check |
| Stage 9 | `IR-1` (`CD-2`) | `egm-implementation-readiness`, `egm-codebase-details` | 🔴 Blocker | `CR-1` | Fix `azure-kv-integration` active account fallback when `kraken_session` is not persisted |
| Stage 9 | `IR-2` (`CD-3`) | `egm-implementation-readiness`, `egm-codebase-details` | 🔴 Blocker | `CR-2` | Decode JWT claims deterministically and define `msTokenPayload` struct |
| Stage 9 | `IR-3` (`CD-4`) | `egm-implementation-readiness`, `egm-codebase-details` | 🔴 Blocker | `CR-3` | Add `EntraBaseURL` and `HTTPClient` to `MicrosoftSessionBroker` for `httptest.NewServer` |
| Stage 9 | `IR-4` (`CD-1`) | `egm-implementation-readiness`, `egm-codebase-details` | 🔴 Blocker | `CR-4` | Remove `syncPluginServerBindings` account-filtering instruction in `manager.go` |
| Stage 9 | `IR-5` (`CD-5`) | `egm-implementation-readiness`, `egm-codebase-details` | 🟡 Major | `CR-9` | Preserve 3-arg `auth.NewHandler` signature and add `SetMicrosoftConfig` |
| Stage 9 | `IR-6` (`CD-6`) | `egm-codebase-details` | 🟡 Major | `CR-10` | Populate `AuthProvider` and `MicrosoftAccount` on `UserResponse` in `PollMicrosoftDeviceLogin` |
| Stage 9 | `IR-7` (`CD-7`) | `egm-implementation-readiness`, `egm-codebase-details` | 🟡 Major | `CR-11` | List `wasm_runtime_test.go` and `backend/plugin.wasm` in Phase 2 & Phase 4 file allowlists |
| Stage 9 | `IR-8` | `egm-implementation-readiness` | 🟡 Major | `CR-12` | Drop `PermissionAuthMicrosoft` and use existing `credentials:resolve` / `credentials:manage` |
| Stage 9 | `IR-9` (`CD-8`) | `egm-codebase-details` | 🟢 Nit | `CR-15` | Note `PluginSurfaceRenderer.tsx` `useEffect` dependency behavior |
| Stage 9 | `IR-10` | `egm-implementation-readiness` | 🟢 Nit | `CR-14` | Restore `accountVaultRegistry` inside `detectKrakenUser()` in `frontend/worker.js` |
| Stage 9 | `IR-11` (`ST-5`, `ST-6`) | `egm-style` | 🟢 Nit | `CR-16` | Remove filler words (`cleanly`, `full`) in `DetailedRange` |
