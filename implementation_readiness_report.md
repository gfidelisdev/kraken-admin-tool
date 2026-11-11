# Stage 9: Implementation Readiness Test (`implementation_readiness_report.md`)

**Target Document**: [`design_draft.md`](file:///home/gfidelis/Code/go/kraken-admin-tool/design_draft.md)
**Evaluated Scope**: `DetailedRange` (`172..381`)
**Reviewers**: `egm-implementation-readiness`, `egm-codebase-details`, `egm-style` (`DetailedRange`)

---

## Executive Summary

Stage 9 evaluated the step-by-step execution instructions in [`design_draft.md`](file:///home/gfidelis/Code/go/kraken-admin-tool/design_draft.md#L172-L381) against the live codebases in `/home/gfidelis/Code/go/kraken-admin-tool` and `/home/gfidelis/Code/kraken-plugins/azure-kv-integration` for zero-decision readiness, codebase alignment, and mechanical style compliance.

- **🔴 Blockers**: 4 (`IR-1`, `IR-2`, `IR-3`, `IR-4`)
- **🟡 Majors**: 4 (`IR-5`, `IR-6`, `IR-7`, `IR-8`)
- **🟢 Nits**: 3 (`IR-9`, `IR-10`, `IR-11`)

---

## 🔴 Blockers

### IR-1: Broken Active Account Selection & Token Resolution When `kraken_session` Accounts Are Stripped from Persistence
* **Source Personas**: `egm-implementation-readiness` (Finding 1), `egm-codebase-details` (Finding 2)
* **Affected Lines**: [`design_draft.md`](file:///home/gfidelis/Code/go/kraken-admin-tool/design_draft.md#L135), [`design_draft.md`](file:///home/gfidelis/Code/go/kraken-admin-tool/design_draft.md#L340-L360)
* **Codebase Reference**: [`/home/gfidelis/Code/kraken-plugins/azure-kv-integration/backend/main.go`](file:///home/gfidelis/Code/kraken-plugins/azure-kv-integration/backend/main.go#L151-L319)
* **Finding**:
  In Phase 4 Item 16 (line 359), `persistConfig()` in `frontend/worker.js` strips all accounts with `source === "kraken_session"` before writing `azure_kv_config` to `PluginKVStore`. In [`backend/main.go`](file:///home/gfidelis/Code/kraken-plugins/azure-kv-integration/backend/main.go#L151-L176), `mergeWithPersistedConfig(incoming *Config)` ignores `incoming.Accounts` and retains only `persisted.Accounts` from `PluginKVStore`. Furthermore, [`getActiveAccount(cfg)`](file:///home/gfidelis/Code/kraken-plugins/azure-kv-integration/backend/main.go#L263-L273) falls back to `return &cfg.Accounts[0]` whenever `cfg.ActiveAccountID` does not match an entry in `cfg.Accounts`.
  Consequently, `cfg.Accounts` in `backend/main.go` will never contain an account with `Source == "kraken_session"`. If any standalone `device_login` account exists in persisted `azure_kv_config`, `getActiveAccount(cfg)` falls back to `&cfg.Accounts[0]` (`Source == "device_login"`), causing the check in Item 15 (`if acc == nil || acc.Source == "kraken_session"`) to evaluate to `false` and silently use the standalone account instead of the active Kraken Microsoft session.
* **Actionable Guidance**:
  1. In Phase 4 Item 15 (`backend/main.go`), define a helper `findExplicitStandaloneAccount(cfg *Config) *MicrosoftAccount` that returns a persisted account from `cfg.Accounts` only when `cfg.ActiveAccountID != ""` and `cfg.ActiveAccountID != "kraken-ms-session"` and `cfg.Accounts[i].ID == cfg.ActiveAccountID` with `cfg.Accounts[i].Source != "kraken_session"`.
  2. Under `case "user_account":` in `buildAuthTokenScript` and `buildARMTokenScript`, if `findExplicitStandaloneAccount(cfg)` is `nil`, call `wasmGetMicrosoftSessionToken(...)` first (`"https://vault.azure.net/.default"` or `"https://management.azure.com/.default"`). If non-empty, emit `AKV_TOKEN=<shellQuotedToken>` / `ARM_TOKEN=<shellQuotedToken>`.
  3. Only if `wasmGetMicrosoftSessionToken(...)` returns `""` (local Kraken session) or an explicit standalone account was selected should `buildAuthTokenScript` and `buildARMTokenScript` fall back to `getActiveAccount(cfg)` and refresh token exchange.
  4. Standardize the helper name to `wasmGetMicrosoftSessionToken` across line 135 and line 345.

---

### IR-2: OIDC `userinfo` Audience Mismatch & Unspecified `msTokenPayload` Struct Definition
* **Source Personas**: `egm-implementation-readiness` (Finding 2), `egm-codebase-details` (Finding 3)
* **Affected Lines**: [`design_draft.md`](file:///home/gfidelis/Code/go/kraken-admin-tool/design_draft.md#L221-L228)
* **Codebase Reference**: [`/home/gfidelis/Code/kraken-plugins/azure-kv-integration/backend/main.go`](file:///home/gfidelis/Code/kraken-plugins/azure-kv-integration/backend/main.go#L412-L448)
* **Finding**:
  1. Phase 1 Item 4 (line 218) requests `scope=openid profile email offline_access https://vault.azure.net/.default`. In Microsoft Entra ID v2.0, combining an OIDC scope with a resource scope (`https://vault.azure.net/.default`) produces an `access_token` whose audience (`aud`) is `https://vault.azure.net`. Passing that `access_token` to `GET https://graph.microsoft.com/oidc/userinfo` (line 227) fails with `401 Unauthorized (Invalid Audience)`.
  2. Line 227 states *"validates identity via `GET https://graph.microsoft.com/oidc/userinfo` (or token claims `preferred_username`, `email`, `upn`, `name`, `tid`)"*, leaving an implementation choice (`or`) that violates the Zero-Decision Rule.
  3. Line 227 references `*msTokenPayload` in `ExtractMicrosoftIdentity(tokenPayload *msTokenPayload)` without defining the struct fields of `msTokenPayload`.
* **Actionable Guidance**:
  1. Define `msTokenPayload` explicitly in Item 4 (`backend/internal/auth/ms_broker.go`) with fields `AccessToken string`, `RefreshToken string`, `IDToken string`, `ExpiresIn int`, `Error string`, and `ErrorDesc string`.
  2. Remove the `GET https://graph.microsoft.com/oidc/userinfo` alternative and specify deterministic base64url JWT payload decoding of `tokenPayload.IDToken` (merging claims from `tokenPayload.AccessToken` if `IDToken` is empty or missing claims):
     - `email`: first non-empty claim containing `"@"` among `email`, `preferred_username`, `upn`, `unique_name` (return an error if none contains `"@"`).
     - `name`: first non-empty claim among `name`, `given_name`, falling back to the email local-part before `"@"`.
     - `tenantID`: `tid` claim, falling back to the configured `fallbackTenant`.
     - `id`: `oid` or `sub` claim, falling back to `"kraken-ms-" + email`.

---

### IR-3: Missing Test Endpoint Override on `MicrosoftSessionBroker` for `httptest.NewServer`
* **Source Personas**: `egm-implementation-readiness` (Finding 2), `egm-codebase-details` (Finding 4)
* **Affected Lines**: [`design_draft.md`](file:///home/gfidelis/Code/go/kraken-admin-tool/design_draft.md#L195-L228), [`design_draft.md`](file:///home/gfidelis/Code/go/kraken-admin-tool/design_draft.md#L245-L254)
* **Finding**:
  Phase 1 Item 3 & Item 4 hardcode `https://login.microsoftonline.com/{tenant}/oauth2/v2.0/devicecode` and `https://login.microsoftonline.com/{tenant}/oauth2/v2.0/token`, while Item 6 (`backend/internal/auth/ms_broker_test.go`) requires unit testing `StartMicrosoftDeviceLogin`, `PollMicrosoftDeviceLogin`, and `GetScopedToken` via a local `httptest.NewServer` mock without external network access. Without a configurable base URL field on `MicrosoftSessionBroker`, tests cannot redirect Entra ID calls to `httptest.NewServer`.
* **Actionable Guidance**:
  Add `EntraBaseURL string` (initialized to `"https://login.microsoftonline.com"` in `NewMicrosoftSessionBroker()`) and `HTTPClient *http.Client` (initialized to `&http.Client{Timeout: 15 * time.Second}`) to `MicrosoftSessionBroker` in `backend/internal/auth/ms_broker.go`, and construct OAuth2 URLs via `strings.TrimRight(b.EntraBaseURL, "/") + "/" + tenant + "/oauth2/v2.0/..."`.

---

### IR-4: Misplaced `syncPluginServerBindings` Account Filtering Instruction in `backend/internal/plugins/manager.go`
* **Source Personas**: `egm-implementation-readiness` (Finding 4), `egm-codebase-details` (Finding 1)
* **Affected Lines**: [`design_draft.md`](file:///home/gfidelis/Code/go/kraken-admin-tool/design_draft.md#L276)
* **Codebase Reference**: [`backend/internal/plugins/manager.go`](file:///home/gfidelis/Code/go/kraken-admin-tool/backend/internal/plugins/manager.go#L658-L676), [`backend/internal/plugins/manager.go`](file:///home/gfidelis/Code/go/kraken-admin-tool/backend/internal/plugins/manager.go#L1316-L1371)
* **Finding**:
  Phase 2 Item 8 (line 276) states: *"In `syncPluginServerBindings(pluginID, rawConfigJSON)`, ensure any account entry with `Source == "kraken_session"` is ignored when persisting server bindings."* Inspection of [`syncPluginServerBindings`](file:///home/gfidelis/Code/go/kraken-admin-tool/backend/internal/plugins/manager.go#L1316-L1371) shows it only unmarshals `ServerBindings map[string]string` and updates `models.DockerServer` rows (`SSHKeySecret` / `PasswordSecret`); it never reads or writes `accounts`.
* **Actionable Guidance**:
  Remove the `syncPluginServerBindings` account-filtering instruction on line 276. If defense-in-depth backend sanitization of `azure_kv_config` is desired in `manager.go`, specify a helper `stripEphemeralSessionAccounts(rawConfigJSON string) string` invoked when `CallPlugin` (`kv_set`) or `WasmHostBridge.OnKVSet` (`kraken_kv_set`) writes key `"azure_kv_config"` or `"com.kraken.azure-keyvault:azure_kv_config"` to `PluginKVStore`, or rely solely on `persistConfig()` in `frontend/worker.js` (Item 16).

---

## 🟡 Majors

### IR-5: `auth.NewHandler` Signature Compatibility With Existing Unit Tests
* **Source Personas**: `egm-implementation-readiness` (Finding 4), `egm-codebase-details` (Finding 5)
* **Affected Lines**: [`design_draft.md`](file:///home/gfidelis/Code/go/kraken-admin-tool/design_draft.md#L229-L233), [`design_draft.md`](file:///home/gfidelis/Code/go/kraken-admin-tool/design_draft.md#L288-L294)
* **Codebase Reference**: [`backend/internal/auth/handler.go`](file:///home/gfidelis/Code/go/kraken-admin-tool/backend/internal/auth/handler.go#L27-L39), [`backend/internal/auth/handler_test.go`](file:///home/gfidelis/Code/go/kraken-admin-tool/backend/internal/auth/handler_test.go#L37)
* **Finding**:
  [`backend/internal/auth/handler_test.go`](file:///home/gfidelis/Code/go/kraken-admin-tool/backend/internal/auth/handler_test.go) calls `auth.NewHandler(db, "test-secret", false)` at 8 call sites. Changing the signature of `NewHandler` would break `handler_test.go`.
* **Actionable Guidance**:
  Specify in Phase 1 Item 5 and Phase 2 Item 10 that `NewHandler(db *gorm.DB, secret string, allowRegistration bool) *Handler` retains its 3-parameter signature (initializing `MSBroker: NewMicrosoftSessionBroker()`, `MicrosoftClientID: DefaultMicrosoftClientID`, `MicrosoftTenantID: DefaultMicrosoftTenantID`) and adds `func (h *Handler) SetMicrosoftConfig(clientID, tenantID string)` to override defaults in `backend/cmd/server/main.go`.

---

### IR-6: Incomplete `UserResponse` Metadata & Initial User Role Alignment in `PollMicrosoftDeviceLogin`
* **Source Personas**: `egm-codebase-details` (Finding 6)
* **Affected Lines**: [`design_draft.md`](file:///home/gfidelis/Code/go/kraken-admin-tool/design_draft.md#L222-L225)
* **Codebase Reference**: [`backend/internal/auth/handler.go`](file:///home/gfidelis/Code/go/kraken-admin-tool/backend/internal/auth/handler.go#L97-L115), [`backend/internal/auth/handler.go`](file:///home/gfidelis/Code/go/kraken-admin-tool/backend/internal/auth/handler.go#L211-L220)
* **Finding**:
  1. In Phase 1 Item 4 (line 225), `PollMicrosoftDeviceLogin` returns `MicrosoftPollResponse` containing `User: &userResp`. Because [`toUserResponse(user)`](file:///home/gfidelis/Code/go/kraken-admin-tool/backend/internal/auth/handler.go#L211-L220) only copies `ID`, `Email`, `Name`, `Role`, `Status`, and `CreatedAt`, `userResp.AuthProvider` and `userResp.MicrosoftAccount` would be empty in the login response unless explicitly populated before returning.
  2. Line 223 creates unrecognized users with `Role: "viewer"`, `Status: "pending"`, whereas [`Handler.Register`](file:///home/gfidelis/Code/go/kraken-admin-tool/backend/internal/auth/handler.go#L97-L115) assigns `Role: "pending"`, `Status: "pending"` (or `Role: "admin"`, `Status: "active"` when `userCount == 0`).
* **Actionable Guidance**:
  In Phase 1 Item 4 (`PollMicrosoftDeviceLogin`), specify that after `userResp := toUserResponse(user)`, the handler sets `userResp.AuthProvider = "microsoft"` and `userResp.MicrosoftAccount = h.MSBroker.GetAccountMetadata(sessionID)` before returning `MicrosoftPollResponse`, and align new user provisioning with `Handler.Register` (`Role: "pending"`, `Status: "pending"`, plus explicit handling for `userCount == 0` if desired).

---

### IR-7: Missing Target Files in Phase 2 and Phase 4 File Allowlists (`wasm_runtime_test.go` and `backend/plugin.wasm`)
* **Source Personas**: `egm-implementation-readiness` (Finding 3), `egm-codebase-details` (Finding 7)
* **Affected Lines**: [`design_draft.md`](file:///home/gfidelis/Code/go/kraken-admin-tool/design_draft.md#L257-L295), [`design_draft.md`](file:///home/gfidelis/Code/go/kraken-admin-tool/design_draft.md#L338-L373)
* **Finding**:
  1. Verification Plan line 373 requires a *"WASM host bridge token retrieval test"* in `./internal/plugins/...`, and Phase 2 modifies `wasm_runtime.go`, `manager.go`, and `manifest.go`, but [`backend/internal/plugins/wasm_runtime_test.go`](file:///home/gfidelis/Code/go/kraken-admin-tool/backend/internal/plugins/wasm_runtime_test.go) is not listed as a target file in Phase 2.
  2. Phase 4 Item 16 (line 360) and Verification Plan (line 367) recompile `/home/gfidelis/Code/kraken-plugins/azure-kv-integration/backend/plugin.wasm`, which is tracked in git, but `backend/plugin.wasm (Modify)` is not listed as an explicit numbered file target in Phase 4.
* **Actionable Guidance**:
  Add `backend/internal/plugins/wasm_runtime_test.go (Modify)` as an explicit item in Phase 2 and `/home/gfidelis/Code/kraken-plugins/azure-kv-integration/backend/plugin.wasm (Modify)` as an explicit numbered item in Phase 4.

---

### IR-8: Speculative `auth:microsoft` Permission Requirement in `wasm_runtime.go`
* **Source Personas**: `egm-zoom-out` (Finding 3, impacts Phase 2 Item 7 & Item 8)
* **Affected Lines**: [`design_draft.md`](file:///home/gfidelis/Code/go/kraken-admin-tool/design_draft.md#L261), [`design_draft.md`](file:///home/gfidelis/Code/go/kraken-admin-tool/design_draft.md#L268), [`design_draft.md`](file:///home/gfidelis/Code/go/kraken-admin-tool/design_draft.md#L275)
* **Codebase Reference**: [`backend/internal/plugins/manifest.go`](file:///home/gfidelis/Code/go/kraken-admin-tool/backend/internal/plugins/manifest.go#L40-L46), [`/home/gfidelis/Code/kraken-plugins/azure-kv-integration/plugin.json`](file:///home/gfidelis/Code/kraken-plugins/azure-kv-integration/plugin.json#L19-L26)
* **Finding**:
  Phase 2 Item 7 (`backend/internal/plugins/manifest.go`) registers a new permission constant `PermissionAuthMicrosoft = "auth:microsoft"`, while Item 8 (`wasm_runtime.go`) checks `auth:microsoft`, `credentials:resolve`, or `credentials:manage` because `azure-kv-integration/plugin.json` already declares `credentials:resolve` and `credentials:manage`. Adding `PermissionAuthMicrosoft` when no plugin manifest declares it adds dead permission surface.
* **Actionable Guidance**:
  Drop Item 7 (`backend/internal/plugins/manifest.go`) and check `m.hasPermission(pluginID, PermissionCredentialsResolve) || m.hasPermission(pluginID, PermissionCredentialsManage)` in `OnGetSessionToken`.

---

## 🟢 Nits

### IR-9: `PluginSurfaceRenderer.tsx` `useEffect` Dependency Array Note
* **Source Personas**: `egm-codebase-details` (Finding 8)
* **Affected Lines**: [`design_draft.md`](file:///home/gfidelis/Code/go/kraken-admin-tool/design_draft.md#L332-L336)
* **Codebase Reference**: [`frontend/src/components/PluginSurfaceRenderer.tsx`](file:///home/gfidelis/Code/go/kraken-admin-tool/frontend/src/components/PluginSurfaceRenderer.tsx#L1403-L1486)
* **Finding**:
  In `PluginSurfaceRenderer.tsx`, the worker bootstrap `useEffect` depends on `[plugin.id, plugin.frontend_bundle_url, surface]`. Reading `useAuth()` inside `useEffect` to pass `user` in the `"init"` message works on mount without recreating the worker on unrelated re-renders, which should be explicitly noted so implementers do not add `user` to the worker-recreation dependency array unnecessarily.
* **Actionable Guidance**:
  Note in Phase 3 Item 14 that `user` is read from `useAuth()` at worker initialization time without adding `user` to the worker teardown `useEffect` dependency list.

---

### IR-10: `detectKrakenUser()` Vault Registry Restoration Order in `frontend/worker.js`
* **Source Personas**: `egm-implementation-readiness` (Finding 5)
* **Affected Lines**: [`design_draft.md`](file:///home/gfidelis/Code/go/kraken-admin-tool/design_draft.md#L352-L360)
* **Codebase Reference**: [`/home/gfidelis/Code/kraken-plugins/azure-kv-integration/frontend/worker.js`](file:///home/gfidelis/Code/kraken-plugins/azure-kv-integration/frontend/worker.js#L498-L518), [`/home/gfidelis/Code/kraken-plugins/azure-kv-integration/frontend/worker.js`](file:///home/gfidelis/Code/kraken-plugins/azure-kv-integration/frontend/worker.js#L3011-L3015)
* **Finding**:
  In `frontend/worker.js`, `await loadConfig()` runs before `detectKrakenUser()` at lines 3011–3015. Because `loadConfig()` checks `getActiveAccount()` to restore `vault_name`, `vault_url`, and `discoveredVaults` from `state.accountVaultRegistry[emailKey]`, injecting the `kraken_session` account inside `detectKrakenUser()` after `loadConfig()` won't restore the vault registry unless `detectKrakenUser()` also applies the `state.accountVaultRegistry[emailKey]` lookup.
* **Actionable Guidance**:
  Specify in Phase 4 Item 16 that `detectKrakenUser()` also restores `state.config.vault_name`, `state.config.vault_url`, and `state.discoveredVaults` from `state.accountVaultRegistry[emailKey]` (or `state.accountVaultRegistry["__last_configured__"]`) when empty.

---

### IR-11: Style & Prose Tightening in `DetailedRange` (`172..381`)
* **Source Personas**: `egm-style` (Findings 5 & 6)
* **Affected Lines**: [`design_draft.md`](file:///home/gfidelis/Code/go/kraken-admin-tool/design_draft.md#L224), [`design_draft.md`](file:///home/gfidelis/Code/go/kraken-admin-tool/design_draft.md#L329)
* **Finding**:
  Line 224 uses filler adverb *"cleanly"* (*"logs in cleanly without modifying"*), and line 329 uses *"full"* (*"Receives the full `User` object"*).
* **Actionable Guidance**:
  - Line 224: Replace *"logs in cleanly without modifying `user.PasswordHash`"* with *"logs in without modifying `user.PasswordHash`"*.
  - Line 329: Replace *"Receives the full `User` object"* with *"Receives the `User` object"*.
