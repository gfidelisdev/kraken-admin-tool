# Microsoft Device Code Authentication and Session Token Broker

*   **Author:** Guilherme Fidelis da Silva
*   **Status:** Draft
*   **Date:** 2026-11-09

--------------------------------------------------------------------------------

## Overview

Kraken Admin Tool is a full-stack control plane for managing Docker containers, host servers, and Kubernetes (K8s) clusters, extensible via WebAssembly (WASM, a portable binary instruction format executed in a sandboxed runtime) and Web Worker plugins. Today, Kraken Admin Tool authenticates operators exclusively through local database accounts (username, password, and optional Time-based One-Time Password, or TOTP), while plugins that interact with Microsoft Azure services—such as the Azure Key Vault Integration plugin (`com.kraken.azure-keyvault`)—must run their own separate Microsoft OAuth2 (Open Authorization 2.0) login flow inside the plugin interface.

This design introduces Microsoft OAuth2 v2.0 Device Code Flow authentication directly on the Kraken Admin Tool sign-in page, coupled with a session-scoped Backend Token Broker. When an operator signs in to Kraken with a Microsoft account, Kraken verifies their local Role-Based Access Control (RBAC) status, binds the resulting Microsoft OAuth2 tokens strictly to their active backend session, and brokers resource-scoped access tokens on demand to authorized plugins through the WASM host bridge. When an operator signs in with a local Kraken account, no Microsoft session token exists in the broker, and plugins fall back to their standalone authentication flows.

## The Problem

Operators who use both Kraken Admin Tool and Microsoft Azure resources currently face three operational and security problems:

1.  **Duplicate Authentication Workflows**: Operators must sign in locally to Kraken Admin Tool and then complete a second Microsoft Device Code Flow inside plugins such as `com.kraken.azure-keyvault`. The plugin executes Azure operations by generating short-lived bash `curl` scripts executed via the Kraken host runner; without centralized authentication, each plugin must maintain independent credentials.
2.  **Cross-User Credential Bleed in Shared Plugin Storage**: The plugin key-value store (`PluginKVStore`) is keyed only by plugin identifier (`plugin_id`) and key name, making it global across all Kraken users. If a plugin persists a user's Microsoft Single Sign-On (SSO) tokens into `PluginKVStore`, a second operator signing in with a local account on the same Kraken instance would inherit or overwrite the first operator's Microsoft credentials.
3.  **Single-Audience Token Limitation vs. Untrusted Worker Exposure**: Microsoft Entra ID (formerly Azure Active Directory) OAuth2 v2.0 issues each `access_token` for a single target resource audience—for example, Azure Key Vault (`https://vault.azure.net/.default`) or Azure Resource Manager (ARM, `https://management.azure.com/.default`). Because a plugin often requires tokens for multiple Azure resources during a single workflow, passing a single static `access_token` is insufficient, whereas exposing the long-lived `refresh_token` to browser Web Workers or unisolated plugin storage creates a token exfiltration risk.

The proposed solution resolves all three issues by adding Microsoft Device Code Flow to Kraken's primary login screen, governing new Microsoft accounts with Kraken's existing administrator approval workflow (`pending` status), holding the Microsoft `refresh_token` exclusively in an ephemeral backend session store, and brokering short-lived, resource-scoped `access_token`s to authorized plugin backends via the WASM host bridge.

## Goals

*   Allow operators to sign in to Kraken Admin Tool using a Microsoft account via the OAuth2 v2.0 Device Code Flow (displaying a user code and verification link on the login screen while polling for completion).
*   Enforce Kraken's RBAC governance on Microsoft sign-ins: existing `active` local users matched by email sign in immediately, while unrecognized emails create a new local user record with `status = "pending"` and `role = "pending"`, blocking sign-in until an administrator approves the account.
*   Retain the user's Microsoft OAuth2 tokens (`access_token` and `refresh_token`) strictly in an in-memory backend session store bound to the user's active Kraken JSON Web Token (JWT) session ID (`jti`, JWT ID), clearing them immediately upon logout or session expiration.
*   Expose only non-sensitive Microsoft session metadata (`auth_provider: "microsoft"`, `email`, `name`, `tenant_id`, and `source: "kraken_session"`) to the frontend application and plugin Web Workers (`self.kraken.user`).
*   Provide a permission-gated Backend Token Broker on the WASM host bridge (`kraken_auth_get_token`) so authorized plugins can request scoped Microsoft access tokens (such as Key Vault or ARM) during a Microsoft-authenticated Kraken session.
*   Ensure that when an operator is signed in with a local Kraken account, plugins receive no `kraken_session` token and fall back to their own standalone authentication configurations.

## Non-Goals

*   Implementing browser redirect-based OAuth2 Authorization Code Flow requiring public callback URLs or mandatory custom Entra ID client secrets.
*   Persisting Kraken-level Microsoft SSO tokens into the SQLite/PostgreSQL database or global `PluginKVStore` across user logouts.
*   Removing or replacing local username/password authentication, local TOTP two-factor authentication, or standalone plugin authentication methods (Service Principal, Managed Identity, Azure CLI, or plugin-local Device Code login).

## Open Questions

*   None. All scope, governance, and token lifecycle questions were resolved during the brainstorming and review phases.

## Critical User Journeys (CUJs)

### CUJ 1: First-Time Microsoft Sign-In (Pending Administrator Approval)

*   **Given**: An operator with `alice@contoso.com` does not yet have a `User` record in Kraken Admin Tool.
*   **When**: Alice clicks "Sign in with Microsoft" on the Kraken login page, copies the displayed device code, authorizes the code at Microsoft's verification page, and Kraken's login page polls the backend completion endpoint.
*   **Then**: Kraken decodes the validated Microsoft token claims, creates a `models.User` record for `alice@contoso.com` with `status = "pending"` and `role = "pending"`, discards the OAuth2 tokens without creating an active session, and returns HTTP 403 (`pending`), displaying the pending administrator approval notice on the login screen.

### CUJ 2: Approved Microsoft Sign-In and Automatic Plugin SSO

*   **Given**: An administrator has approved `alice@contoso.com` (or Alice already had an `active` account with matching email and role `admin` or `operator`), and the `com.kraken.azure-keyvault` plugin is enabled.
*   **When**: Alice signs in via the Microsoft Device Code Flow on the Kraken login page and navigates to the Azure Key Vault plugin page.
*   **Then**:
    1. Kraken issues a Kraken JWT containing a unique session ID (`jti`) and `auth_provider = "microsoft"`, and stores Alice's Microsoft OAuth2 tokens in the backend session broker indexed by `jti`.
    2. `PluginSurfaceRenderer` injects Alice's public user profile (including `auth_provider: "microsoft"` and `microsoft_account` metadata with `source: "kraken_session"`) into `self.kraken.user` without any raw OAuth2 tokens.
    3. The Azure Key Vault plugin detects the active `kraken_session` account and, when discovering vaults or listing secrets, requests scoped tokens (`https://management.azure.com/.default` and `https://vault.azure.net/.default`) from Kraken's backend Token Broker via the WASM host bridge (`kraken_auth_get_token`) without prompting Alice to log in again.

### CUJ 3: Local User Sign-In with Plugin Standalone Fallback

*   **Given**: An operator signs in to Kraken Admin Tool using a local username and password (`admin`), and opens the Azure Key Vault plugin.
*   **When**: The plugin initializes and checks `self.kraken.user` and the backend Token Broker.
*   **Then**: `self.kraken.user.auth_provider` is `"local"` and the backend Token Broker reports no Microsoft session for the current JWT; the plugin uses its own persisted standalone configuration (or prompts the operator to connect a Microsoft account or Service Principal inside the plugin).

## Technical Plan (Architecture)

The architecture separates authentication into three boundaries: **Identity Verification & Governance** (`backend/internal/auth`), **Ephemeral Session Token Brokering** (`backend/internal/auth/ms_broker.go`), and **Permission-Gated Plugin Delegation** (`backend/internal/plugins` and `frontend/src/components/plugins/PluginSurfaceRenderer.tsx`).

```text
+-----------------------------------------------------------------------------------+
| Browser (Next.js Frontend & Isolated Plugin Web Worker)                           |
|                                                                                   |
|  [LoginPage] ---> POST /api/auth/microsoft/device-code                            |
|              ---> POST /api/auth/microsoft/poll                                   |
|                        |                                                          |
|                        v                                                          |
|  [AuthContext] <-- Kraken JWT + UserResponse (auth_provider="microsoft",          |
|                        |                      microsoft_account metadata ONLY)    |
|                        v                                                          |
|  [PluginSurfaceRenderer] ---> self.kraken.user (NO access_token / refresh_token)  |
|                        |                                                          |
|                        +---> POST /api/plugins/{id}/call (Bearer Kraken JWT)      |
+----------------------------------------|------------------------------------------+
                                         |
                                         v
+-----------------------------------------------------------------------------------+
| Kraken Go Backend                                                                 |
|                                                                                   |
|  [AuthMiddleware] ---> Parses JWT Claims (UserID, Username, Role, AuthProvider,   |
|                        SessionID / jti) into request context.Context              |
|                                         |                                         |
|                                         v                                         |
|  [plugins.Manager.CallPlugin] ---> Instantiates WasmHostBridge with SessionID     |
|                                         |                                         |
|                                         v                                         |
|  [WASM Runtime: kraken_auth_get_token(scope)]                                     |
|     1. Verifies plugin has 'credentials:resolve' or 'credentials:manage'          |
|     2. Calls MicrosoftSessionBroker.GetScopedToken(ctx, sessionID, scope)         |
|     3. Broker returns cached scoped access_token or exchanges session             |
|        refresh_token against {EntraBaseURL}/{tenant}/oauth2/v2.0/token           |
+-----------------------------------------------------------------------------------+
```

### 1. Microsoft Device Code Flow in `backend/internal/auth`

*   **Client & Tenant Configuration**: Uses the public Microsoft Azure CLI Client ID (`04b07795-8ddb-461a-bbee-02f9e1bf7b46`) and tenant `"common"` by default, overridable via environment variables `MICROSOFT_CLIENT_ID` and `MICROSOFT_TENANT_ID` in `backend/internal/config/config.go`.
*   **Initiation (`POST /api/auth/microsoft/device-code`)**: Sends an HTTP `POST` request from the Go backend (`net/http` client with a 15-second timeout) to `{EntraBaseURL}/{tenant}/oauth2/v2.0/devicecode` with `client_id` and `scope=openid profile email offline_access https://vault.azure.net/.default`. Returns `device_code`, `user_code`, `verification_uri`, `expires_in`, `interval`, and `message` to the frontend.
*   **Polling & Token Validation (`POST /api/auth/microsoft/poll`)**: Exchanges the `device_code` at `{EntraBaseURL}/{tenant}/oauth2/v2.0/token`.
    *   While Entra ID returns `error == "authorization_pending"`, returns HTTP 200 with `{"status": "pending_authorization"}`.
    *   While Entra ID returns `error == "slow_down"`, returns HTTP 200 with `{"status": "slow_down", "interval": interval + 5}` per RFC 8628 §3.5 so the client backs off its polling interval.
    *   Upon successful token issuance, decodes identity claims deterministically from the JWT payload of `id_token` (merging claims from `access_token` if needed): `email` (first non-empty claim with `@` among `email`, `preferred_username`, `upn`, `unique_name`), `name` (`name` or `given_name`, defaulting to email local-part), `tenantID` (`tid`, defaulting to configured tenant), and `id` (`oid` or `sub`, defaulting to `"kraken-ms-" + email`). Entra ID validates the user credentials directly over TLS before issuing the token payload, eliminating audience mismatch issues with Graph API userinfo endpoints.
*   **Local Account Matching & Pending Governance**:
    *   Looks up `models.User` by case-insensitive `email`.
    *   If no user exists, creates a new `models.User` with a unique username derived from the email prefix, a random 32-character bcrypt-hashed password (so local password login cannot be guessed), `Role: "pending"`, and `Status: "pending"`. Logs an audit event and returns HTTP 403 with `status: "pending"`.
    *   If the matched user has `Status == "pending"` or `"rejected"`, returns HTTP 403 matching the existing `Handler.Login` behavior.
    *   If the matched user has `Status == "active"`, generates a unique session identifier (`sessionID`), stores the Microsoft `refresh_token` and initial scoped `access_token` in the in-memory `MicrosoftSessionBroker`, issues a Kraken JWT encoding `AuthProvider: "microsoft"` and `RegisteredClaims.ID = sessionID`, populates `userResp.AuthProvider = "microsoft"` and `userResp.MicrosoftAccount`, and returns `MicrosoftPollResponse{Status: "authenticated", Token: jwtToken, User: &userResp}`.

### 2. Ephemeral Backend Session Token Broker

*   `MicrosoftSessionBroker` is an in-memory store in `backend/internal/auth` keyed by Kraken JWT `SessionID` (`jti`), protected by an internal `sync.RWMutex` for entry lookups, configurable `EntraBaseURL string` (defaulting to `"https://login.microsoftonline.com"`), and `HTTPClient *http.Client` for local test mocking.
*   Each session entry holds: `UserID`, `Email`, `Name`, `TenantID`, `ClientID`, `RefreshToken`, `SessionExpiresAt` (matching the 24-hour JWT lifetime), a map of `ScopedTokens` (`scope -> {AccessToken, ExpiresAt}`), and an internal session-level `sync.Mutex`.
*   **Scope Exchange on Demand & Concurrency**: When a plugin requests a token for a target scope (e.g., `https://vault.azure.net/.default` or `https://management.azure.com/.default`), `GetScopedToken(ctx, sessionID, scope)` checks if a non-expired token (with a 5-minute safety buffer) is cached under a read lock. If refresh is needed, it acquires the session-level lock and releases the global broker map lock before executing the outbound HTTP `POST` refresh call to `{EntraBaseURL}/{tenant}/oauth2/v2.0/token`. This prevents outbound network latency from blocking concurrent requests across other sessions. Upon receipt, it updates the cached scoped token, rotates `RefreshToken` if returned, and returns the short-lived `access_token`.
*   **Logout & Expiration Cleanup**: Calling `POST /api/auth/logout` immediately deletes the session entry. `CreateSession` opportunistically sweeps and evicts all expired session entries where `time.Now().After(entry.SessionExpiresAt)`.

### 3. Plugin Context & Host Bridge Integration

*   **Frontend Worker Metadata (`PluginSurfaceRenderer.tsx`)**: Reads `user` from `useAuth()` at initialization and injects a sanitized `self.kraken.user` object (`id`, `username`, `email`, `role`, `auth_provider`, `microsoft_account: { id, email, name, tenant_id, source: "kraken_session" }`) into the Worker SDK prelude without adding `user` to the worker recreation teardown dependency array. No `access_token` or `refresh_token` crosses into the browser or Web Worker.
*   **WASM Host Bridge (`wasm_runtime.go` & `manager.go`)**:
    *   `WasmHostBridge` receives `UserID`, `Username`, `SessionID`, and a `GetMicrosoftToken func(ctx context.Context, scope string) (string, *models.MicrosoftSessionAccount, error)` callback wired to `MicrosoftSessionBroker`.
    *   The `kraken` host module exports host function `kraken_auth_get_token(scopePtr, scopeLen, outPtr, maxOutLen uint32) uint32` gated on `bridge.Permissions["credentials:resolve"] || bridge.Permissions["credentials:manage"]`.
*   **Plugin Consumption (`com.kraken.azure-keyvault`)**:
    *   In `frontend/worker.js`, when `self.kraken.user?.auth_provider === "microsoft"` and `self.kraken.user?.microsoft_account` is present, `detectKrakenUser()` registers an ephemeral in-memory account entry with `source: "kraken_session"` (stripped before `persistConfig()` writes to `PluginKVStore` or IndexedDB so it never pollutes global storage). If `state.config.vault_name` is empty, it restores vault settings from `state.accountVaultRegistry`.
    *   In `backend/main.go`, `buildAuthTokenScript` and `buildARMTokenScript` under `case "user_account":` check whether an explicit standalone account was selected. If not, they call `wasmGetMicrosoftSessionToken(scope)` first. If a valid token is returned by the host bridge, the script uses `AKV_TOKEN` / `ARM_TOKEN` directly. If empty (local Kraken user) or if an explicit standalone account was chosen, it falls back to standalone accounts in `cfg.Accounts`.

## Alternatives Considered

### Alternative A: Inject Microsoft `access_token` and `refresh_token` Directly into `self.kraken` and `PluginKVStore`

*   **Pros**: Requires no new WASM host function or backend token broker; the plugin worker could reuse its existing client-side token handling directly.
*   **Cons**: Exposes long-lived OAuth2 `refresh_token` credentials to browser JavaScript and Web Workers (vulnerable to cross-site scripting or untrusted plugin code) and writes single-user SSO tokens into the global `PluginKVStore` (`azure_kv_config`), causing other local Kraken users to inherit the first user's Microsoft identity.
*   **Reason for Rejection**: Rejected to enforce least-privilege token isolation and prevent leaking session credentials across users in shared plugin storage.

### Alternative B: Automatically Activate All Microsoft Device Code Logins (`status = "active"`)

*   **Pros**: Allows immediate single-step sign-in for new Microsoft accounts without waiting for an administrator.
*   **Cons**: Because the default public Client ID (`04b07795-8ddb-461a-bbee-02f9e1bf7b46`) with `tenant = "common"` accepts any personal or organizational Microsoft account globally, auto-activation would grant arbitrary external Microsoft account holders access to Kraken read endpoints.
*   **Reason for Rejection**: Rejected to enforce administrator approval before account activation, matching Kraken's local self-registration security model.

### Alternative C: Persist Microsoft Refresh Tokens in the Database Across Sessions

*   **Pros**: Allows unattended background jobs to reuse a user's personal Microsoft token after the user logs out.
*   **Cons**: Turns a personal interactive operator login into a persistent service credential stored on disk, violating the principle that interactive SSO permissions last only while the operator session is active. Unattended jobs already have dedicated support via Service Principal or Managed Identity in the plugin.
*   **Reason for Rejection**: Rejected to enforce session-scoped credential lifetimes.

--------------------------------------------------------------------------------

## Non-Functional Requirements

*   **Security & Privacy**:
    *   OAuth2 `refresh_token` and `access_token` values are never serialized into `models.User`, `models.UserResponse`, `PluginKVStore`, browser `localStorage`, or Web Worker context.
    *   Plugin access to `kraken_auth_get_token` is denied unless the plugin has been granted `credentials:resolve` or `credentials:manage`, and the scope requested matches an allowed Microsoft resource URI (`https://vault.azure.net/.default`, `https://management.azure.com/.default`, or `https://graph.microsoft.com/.default`).
    *   The `/api/auth/microsoft/device-code` endpoint is protected by `authRateLimiter`, while `/api/auth/microsoft/poll` supports RFC 8628 polling and backoff without premature rate limiting.
*   **Performance & Latency**:
    *   Scoped access tokens obtained by `MicrosoftSessionBroker` are cached in memory per `(sessionID, scope)` until 5 minutes before their `expires_in` deadline (typically 55 minutes), avoiding redundant network calls to `login.microsoftonline.com` on repeated plugin actions. Token refresh calls release the global broker lock to prevent blocking concurrent sessions.
*   **Risks & Limits**:
    *   *Backend Restart Session Demotion*: Because `MicrosoftSessionBroker` maintains tokens strictly in-memory, restarting the Kraken backend process invalidates the ephemeral Microsoft session broker state while the 24-hour Kraken JWT remains valid. In this event, `GET /api/auth/me` detects the missing broker session and downgrades `user.auth_provider` to `"local"`. Plugins will fall back to their standalone credentials or prompt for reconnect without crashing.
    *   *Tenant Pinning Recommendation*: When deploying in enterprise production environments, administrators should set `MICROSOFT_TENANT_ID` to their specific Microsoft Entra Tenant GUID rather than `"common"` to restrict logins to corporate directory accounts.
    *   *Initial Bootstrap (`userCount == 0`)*: If the first user on a completely fresh Kraken instance signs in via Microsoft before any local admin account is registered, they follow the same pending registration flow requiring an administrator to activate them, or initial setup should be completed using local admin registration.
*   **Rollout & Rollback**:
    *   The feature requires no destructive database migrations; `models.User` continues to use existing columns, and session tokens live in memory. Rolling back is a binary revert with zero database schema incompatibility.

--------------------------------------------------------------------------------

## Detailed Implementation Plan

The implementation is partitioned into 4 incremental phases so that each phase leaves both workspaces buildable and testable.

### Phase 1: Backend Session Broker, JWT Claims, and Microsoft Device Code Endpoints (`kraken-admin-tool/backend`)

#### 1. `backend/internal/config/config.go` (Modify)

*   **Type / Scope**: Go Configuration Package
*   **Proposed Changes**:
    *   Add `MicrosoftClientID string` and `MicrosoftTenantID string` fields to `Config`.
    *   In `Load()`, read `MICROSOFT_CLIENT_ID` (defaulting to `"04b07795-8ddb-461a-bbee-02f9e1bf7b46"`) and `MICROSOFT_TENANT_ID` (defaulting to `"common"`).

#### 2. `backend/internal/models/models.go` (Modify)

*   **Type / Scope**: Go Data Models & DTOs
*   **Proposed Changes**:
    *   Add `MicrosoftSessionAccount` struct:
        *   `ID string` (`json:"id"`)
        *   `Email string` (`json:"email"`)
        *   `Name string` (`json:"name"`)
        *   `TenantID string` (`json:"tenant_id"`)
        *   `Source string` (`json:"source"`) — set to `"kraken_session"`
    *   Extend `UserResponse` struct with optional session fields:
        *   `AuthProvider string` (`json:"auth_provider,omitempty"`) — `"local"` or `"microsoft"`
        *   `MicrosoftAccount *MicrosoftSessionAccount` (`json:"microsoft_account,omitempty"`)
    *   Add DTOs for Microsoft Device Code Flow:
        *   `MicrosoftDeviceCodeRequest` (`TenantID string`, `ClientID string`)
        *   `MicrosoftDeviceCodeResponse` (`DeviceCode string`, `UserCode string`, `VerificationURI string`, `ExpiresIn int`, `Interval int`, `Message string`)
        *   `MicrosoftPollRequest` (`DeviceCode string`, `TenantID string`, `ClientID string`)
        *   `MicrosoftPollResponse` (`Status string`, `Token string`, `User *UserResponse`, `Interval int`, `Error string`)

#### 3. `backend/internal/auth/jwt.go` (Modify)

*   **Type / Scope**: Go JWT Authentication Package
*   **Proposed Changes**:
    *   Extend `Claims` struct with `AuthProvider string` (`json:"auth_provider,omitempty"`).
    *   Add `GenerateTokenWithSession(user *models.User, secret string, duration time.Duration, authProvider string, sessionID string) (string, error)` that sets `Claims.AuthProvider = authProvider` and `Claims.RegisteredClaims.ID = sessionID`.
    *   Keep `GenerateToken(user, secret, duration)` calling `GenerateTokenWithSession(user, secret, duration, "local", "")` for full backward compatibility with existing tests and callers.

#### 4. `backend/internal/auth/ms_broker.go` (Create)

*   **Type / Scope**: Go Ephemeral OAuth2 Session Token Broker
*   **Proposed Changes**:
    *   Define `DefaultMicrosoftClientID = "04b07795-8ddb-461a-bbee-02f9e1bf7b46"` and `DefaultMicrosoftTenantID = "common"`.
    *   Define `msTokenPayload` struct:
        *   `AccessToken string` (`json:"access_token"`)
        *   `RefreshToken string` (`json:"refresh_token"`)
        *   `IDToken string` (`json:"id_token"`)
        *   `ExpiresIn int` (`json:"expires_in"`)
        *   `Error string` (`json:"error"`)
        *   `ErrorDesc string` (`json:"error_description"`)
    *   Define `ScopedTokenEntry` (`AccessToken string`, `ExpiresAt time.Time`) and `MicrosoftSessionEntry` (`SessionID string`, `UserID uint`, `Email string`, `Name string`, `TenantID string`, `ClientID string`, `RefreshToken string`, `ScopedTokens map[string]ScopedTokenEntry`, `ExpiresAt time.Time`, `mu sync.Mutex`).
    *   Implement `MicrosoftSessionBroker` with `sync.RWMutex`, `sessions map[string]*MicrosoftSessionEntry`, `EntraBaseURL string` (defaulting to `"https://login.microsoftonline.com"`), and `HTTPClient *http.Client` (defaulting to `&http.Client{Timeout: 15 * time.Second}`):
        *   `NewMicrosoftSessionBroker() *MicrosoftSessionBroker`
        *   `CreateSession(userID uint, email, name, tenantID, clientID, refreshToken, initialScope, initialAccessToken string, expiresIn int, sessionTTL time.Duration) string`: sweeps expired sessions where `time.Now().After(entry.ExpiresAt)`, generates a random 16-byte hex `sessionID`, stores the entry, and returns `sessionID`.
        *   `GetAccountMetadata(sessionID string) *models.MicrosoftSessionAccount`: returns non-sensitive `MicrosoftSessionAccount` if the session exists and is not expired; deletes and returns `nil` if expired.
        *   `GetScopedToken(ctx context.Context, sessionID string, scope string) (string, *models.MicrosoftSessionAccount, error)`: validates that `scope` is in the allowed set (`https://vault.azure.net/.default`, `https://management.azure.com/.default`, `https://graph.microsoft.com/.default`). Checks cached tokens under read lock. If refresh is required, acquires session-specific lock, releases global map lock, and executes HTTP `POST` to `{EntraBaseURL}/{tenant}/oauth2/v2.0/token` (`grant_type=refresh_token`, `client_id`, `refresh_token`, `scope={scope} offline_access openid profile email`). Caches the new `AccessToken`, updates `RefreshToken` if rotated, and returns the token.
        *   `DeleteSession(sessionID string)`: wipes token strings and removes `sessionID` from `sessions`.
    *   Implement helper functions for Device Code Flow:
        *   `StartDeviceCodeFlow(ctx context.Context, tenantID, clientID string) (*models.MicrosoftDeviceCodeResponse, error)`
        *   `PollDeviceCodeToken(ctx context.Context, tenantID, clientID, deviceCode string) (*msTokenPayload, string, int, error)` (returns status `"pending_authorization"`, `"slow_down"`, or `"completed"`).
        *   `ExtractMicrosoftIdentity(tokenPayload *msTokenPayload, fallbackTenant string) (id, email, name, tenantID string, err error)`: decodes base64url JWT payload of `tokenPayload.IDToken` (and `tokenPayload.AccessToken`), extracts `email` from first non-empty claim with `@` among `email`, `preferred_username`, `upn`, `unique_name`, `name` from `name` or `given_name` (defaulting to email local-part), `tenantID` from `tid` (defaulting to `fallbackTenant`), and `id` from `oid` or `sub` (defaulting to `"kraken-ms-" + email`).

#### 5. `backend/internal/auth/handler.go` (Modify)

*   **Type / Scope**: Go HTTP Auth Handlers
*   **Proposed Changes**:
    *   Maintain `NewHandler(db *gorm.DB, secret string, allowRegistration bool) *Handler` signature for backward compatibility with existing tests; initialize `MSBroker: NewMicrosoftSessionBroker()`, `MicrosoftClientID: DefaultMicrosoftClientID`, and `MicrosoftTenantID: DefaultMicrosoftTenantID`.
    *   Add `func (h *Handler) SetMicrosoftConfig(clientID, tenantID string)` for override from `main.go`.
    *   Add `StartMicrosoftDeviceLogin(w http.ResponseWriter, r *http.Request)`: handles `POST /api/auth/microsoft/device-code`, calls `StartDeviceCodeFlow`, and returns JSON `MicrosoftDeviceCodeResponse`.
    *   Add `PollMicrosoftDeviceLogin(w http.ResponseWriter, r *http.Request)`: handles `POST /api/auth/microsoft/poll`:
        *   Calls `PollDeviceCodeToken`. If pending, responds HTTP 200 `{"status": "pending_authorization"}`. If slow down, responds HTTP 200 `{"status": "slow_down", "interval": interval}`.
        *   Extracts `(id, email, name, tenantID)`.
        *   Queries `h.DB.Where("LOWER(email) = ?", strings.ToLower(email)).First(&user)`.
        *   If `errors.Is(err, gorm.ErrRecordNotFound)`: generates a unique username from the email local-part, hashes a random 32-char password via `bcrypt.GenerateFromPassword`, creates `models.User{Username: uname, Email: strings.ToLower(email), PasswordHash: string(hash), Role: "pending", Status: "pending"}`, logs `LOGIN_MICROSOFT_PENDING` via `audit.Log`, and responds HTTP 403 with `{"error": "Your Microsoft account has been registered and is pending administrator approval. Please contact an administrator to approve your account and assign your role before signing in.", "status": "pending"}`.
        *   If `user.Status == "pending"` or `user.Status == "rejected"`, logs audit failure and responds HTTP 403 matching `Handler.Login`.
        *   If `user.Status == "active"`, creates a broker session via `h.MSBroker.CreateSession(...)`, generates a Kraken JWT via `GenerateTokenWithSession(&user, h.JWTSecret, 24*time.Hour, "microsoft", sessionID)`, logs `LOGIN_MICROSOFT` `SUCCESS`, populates `userResp := toUserResponse(user)`, sets `userResp.AuthProvider = "microsoft"` and `userResp.MicrosoftAccount = h.MSBroker.GetAccountMetadata(sessionID)`, and returns HTTP 200 with `MicrosoftPollResponse{Status: "authenticated", Token: jwtToken, User: &userResp}`.
    *   Add `Logout(w http.ResponseWriter, r *http.Request)`: handles `POST /api/auth/logout`, reads `claims := GetUserFromContext(r.Context())`, deletes `claims.ID` from `h.MSBroker` if present, and returns HTTP 200 `{"status": "ok"}`.
    *   Update `Me(w http.ResponseWriter, r *http.Request)`: populates `AuthProvider` (`claims.AuthProvider`, defaulting to `"local"`) and `MicrosoftAccount` (`h.MSBroker.GetAccountMetadata(claims.ID)`) on `models.UserResponse`. If `claims.AuthProvider == "microsoft"` but `h.MSBroker.GetAccountMetadata(claims.ID)` is `nil` (for example, after a backend restart), downgrades `AuthProvider` to `"local"`.

#### 6. `backend/internal/auth/ms_broker_test.go` (Create)

*   **Type / Scope**: Go Unit Tests
*   **Proposed Changes**:
    *   Test `StartMicrosoftDeviceLogin` and `PollMicrosoftDeviceLogin` using `httptest.NewServer` by pointing `broker.EntraBaseURL = srv.URL`:
        *   Case 1: `authorization_pending` returns HTTP 200 `{"status": "pending_authorization"}` and `slow_down` returns `{"status": "slow_down"}` with backoff.
        *   Case 2: Unrecognized Microsoft email creates a `models.User` with `status = "pending"` and returns HTTP 403.
        *   Case 3: Existing `active` user receives a valid JWT with `auth_provider = "microsoft"`, stores tokens in `MSBroker`, and `GET /api/auth/me` returns `microsoft_account` metadata without exposing raw OAuth2 tokens.
        *   Case 4: `GetScopedToken` exchanges the refresh token for `https://management.azure.com/.default`, caches the result, and `POST /api/auth/logout` evicts the session.

---

### Phase 2: Backend Plugin Bridge & Server Route Wiring (`kraken-admin-tool/backend`)

#### 7. `backend/internal/plugins/wasm_runtime.go` (Modify)

*   **Type / Scope**: Go Wazero WASM Host Bridge
*   **Proposed Changes**:
    *   Add `GetMicrosoftToken func(ctx context.Context, scope string) (string, *models.MicrosoftSessionAccount, error)` to `WasmHostBridge`.
    *   In `NewWasmRuntimeManager`, export host function `kraken_auth_get_token(ctx context.Context, mod api.Module, scopePtr, scopeLen, outPtr, maxOutLen uint32) uint32` on the `"kraken"` host module:
        *   Checks `bridge != nil && bridge.GetMicrosoftToken != nil && (bridge.Permissions["credentials:resolve"] || bridge.Permissions["credentials:manage"])`.
        *   Reads `scope` from `[scopePtr, scopeLen)`.
        *   Calls `bridge.GetMicrosoftToken(ctx, scope)`. On error or empty token, returns `0`.
        *   Marshals `{"access_token": tok, "account": acc}` as JSON into `[outPtr, maxOutLen)` and returns the number of bytes written.

#### 8. `backend/internal/plugins/manager.go` (Modify)

*   **Type / Scope**: Go Plugin Manager
*   **Proposed Changes**:
    *   Add `msTokenProvider func(ctx context.Context, scope string) (string, *models.MicrosoftSessionAccount, error)` field to `Manager` and setter `SetMicrosoftTokenProvider(fn func(ctx context.Context, scope string) (string, *models.MicrosoftSessionAccount, error))`.
    *   In `CallPlugin`, pass `GetMicrosoftToken: m.msTokenProvider` when constructing `WasmHostBridge`.

#### 9. `backend/internal/plugins/wasm_runtime_test.go` (Modify)

*   **Type / Scope**: Go Unit Tests
*   **Proposed Changes**:
    *   Add unit test verifying `kraken_auth_get_token` host function invocation across WASM boundary:
        *   Case 1: When bridge has `credentials:resolve` permission and broker returns token, WASM receives valid JSON payload.
        *   Case 2: When bridge lacks required permissions, returns 0 bytes.
        *   Case 3: When broker returns empty token (local user), returns 0 bytes without panic.

#### 10. `backend/cmd/server/main.go` (Modify)

*   **Type / Scope**: Go HTTP Server Entrypoint
*   **Proposed Changes**:
    *   Call `authHandler.SetMicrosoftConfig(cfg.MicrosoftClientID, cfg.MicrosoftTenantID)`.
    *   Wire `pluginManager.SetMicrosoftTokenProvider` using `auth.GetUserFromContext(ctx)` and `authHandler.MSBroker.GetScopedToken(ctx, claims.ID, scope)`.
    *   Register public auth routes under `/api/auth`:
        *   `POST /api/auth/microsoft/device-code` -> `authHandler.StartMicrosoftDeviceLogin` (with `authRateLimiter`)
        *   `POST /api/auth/microsoft/poll` -> `authHandler.PollMicrosoftDeviceLogin` (exempt from strict 0.5 req/s rate limiter to allow normal RFC 8628 polling)
    *   Register protected route under `/api/auth`:
        *   `POST /api/auth/logout` -> `authHandler.Logout`

---

### Phase 3: Frontend Microsoft Device Code Login & Worker Context (`kraken-admin-tool/frontend`)

#### 11. `frontend/src/types/index.ts` (Modify)

*   **Type / Scope**: TypeScript Interfaces
*   **Proposed Changes**:
    *   Add `MicrosoftSessionAccount` interface (`id: string`, `email: string`, `name: string`, `tenant_id?: string`, `source: "kraken_session"`).
    *   Extend `User` interface with `auth_provider?: "local" | "microsoft" | string` and `microsoft_account?: MicrosoftSessionAccount | null`.
    *   Add `MicrosoftDeviceCodeResponse` and `MicrosoftPollResponse` interfaces.

#### 12. `frontend/src/context/AuthContext.tsx` (Modify)

*   **Type / Scope**: React Authentication Context
*   **Proposed Changes**:
    *   Add `completeMicrosoftLogin: (token: string, user: User) => void` to `AuthContextType` so `LoginPage` can commit the authenticated Microsoft session state directly upon poll completion.
    *   Update `logout` to call `api.post("/api/auth/logout", {}).catch(() => {})` before clearing the local token so the backend evicts the `MicrosoftSessionBroker` entry immediately.
    *   In the initial OTP prompt `useEffect`, skip auto-opening `SetupOTPModal` when `user.auth_provider === "microsoft"`.

#### 13. `frontend/src/app/login/page.tsx` (Modify)

*   **Type / Scope**: Next.js Login Page Component
*   **Proposed Changes**:
    *   Add a "Sign in with Microsoft" button below the local login form (styled consistently with the Kraken dark slate card and Microsoft 4-color logo).
    *   When clicked, calls `POST /api/auth/microsoft/device-code` and transitions the card into the Device Code authorization view displaying:
        *   The `user_code` in a selectable monospace banner.
        *   A "Copy Code & Open Microsoft Login" button opening `verification_uri` (`https://microsoft.com/devicelogin`) in a new tab and copying `user_code` to clipboard.
        *   An automatic polling loop (`setInterval` at `interval * 1000` ms, default 3500 ms) calling `POST /api/auth/microsoft/poll`.
        *   A "Cancel / Use Local Account" button that stops polling and returns to the username/password form.
    *   When `/api/auth/microsoft/poll` returns `status: "slow_down"`, increases the polling interval by `+5000ms`.
    *   When `/api/auth/microsoft/poll` returns HTTP 200 with `status: "authenticated"`, invokes `completeMicrosoftLogin(res.token, res.user)` and navigates to `"/"`.
    *   When `/api/auth/microsoft/poll` returns HTTP 403 with `status: "pending"`, stops polling, sets `isPendingApproval = true`, and displays the pending administrator approval banner.

#### 14. `frontend/src/components/plugins/PluginSurfaceRenderer.tsx` (Modify)

*   **Type / Scope**: React Plugin Web Worker Host Component
*   **Proposed Changes**:
    *   Import `useAuth` from `@/context/AuthContext` and read `const { user } = useAuth()`.
    *   Serialize a sanitized `workerUser` object (`id`, `username`, `email`, `role`, `auth_provider: user?.auth_provider || "local"`, `microsoft_account: user?.microsoft_account || null`) into `self.kraken.user` and `self.kraken.session = { user: workerUser }` inside `sdkPrelude` at worker initialization time, without adding `user` to the worker teardown dependency array (`[plugin.id, plugin.frontend_bundle_url, surface]`).

---

### Phase 4: Azure Key Vault Plugin Integration (`azure-kv-integration`)

#### 15. `/home/gfidelis/Code/kraken-plugins/azure-kv-integration/backend/main.go` (Modify)

*   **Type / Scope**: Go WASM Plugin Backend
*   **Proposed Changes**:
    *   Declare `//go:wasmimport kraken kraken_auth_get_token` `func hostAuthGetToken(scopePtr, scopeLen, outPtr, maxOutLen uint32) uint32`.
    *   Implement `wasmGetMicrosoftSessionToken(scope string) (string, *MicrosoftAccount)`: calls `hostAuthGetToken` with `scope`, unmarshals the returned JSON `{"access_token": ..., "account": ...}`, and returns the scoped token and metadata.
    *   Define helper `findExplicitStandaloneAccount(cfg *Config) *MicrosoftAccount` returning a matching account from `cfg.Accounts` only if `cfg.ActiveAccountID != ""` and `cfg.ActiveAccountID != "kraken-ms-session"` and `cfg.Accounts[i].ID == cfg.ActiveAccountID` with `cfg.Accounts[i].Source != "kraken_session"`.
    *   Update `buildAuthTokenScript(cfg *Config)` and `buildARMTokenScript(cfg *Config)` under `case "user_account":`:
        *   If `findExplicitStandaloneAccount(cfg)` is `nil`, call `wasmGetMicrosoftSessionToken("https://vault.azure.net/.default")` (in `buildAuthTokenScript`) or `wasmGetMicrosoftSessionToken("https://management.azure.com/.default")` (in `buildARMTokenScript`).
        *   If non-empty, emit `AKV_TOKEN=<shellQuotedToken>` (or `ARM_TOKEN=<shellQuotedToken>`) directly without invoking external refresh curl.
        *   If `wasmGetMicrosoftSessionToken` returns empty (local Kraken user) or an explicit standalone account was chosen, fall back to `getActiveAccount(cfg)`. If no usable account exists, return actionable error message.

#### 16. `/home/gfidelis/Code/kraken-plugins/azure-kv-integration/frontend/worker.js` (Modify)

*   **Type / Scope**: Plugin Frontend Web Worker
*   **Proposed Changes**:
    *   Update `detectKrakenUser()` to inspect `self.kraken.user`:
        *   When `user.auth_provider === "microsoft"` and `user.microsoft_account` is present, construct the ephemeral session account `{ id: user.microsoft_account.id || "kraken-ms-session", email: user.microsoft_account.email, name: user.microsoft_account.name, tenant_id: user.microsoft_account.tenant_id || "common", source: "kraken_session" }` and inject it at the front of `state.config.accounts` in memory, setting `state.config.active_account_id` to its `id` if no manual account was explicitly forced. If `state.config.vault_name` is empty, restore configuration from `state.accountVaultRegistry[emailKey]` (or `state.accountVaultRegistry["__last_configured__"]`).
        *   When `user.auth_provider !== "microsoft"` (local Kraken user), strip any stale account with `source === "kraken_session"` from `state.config.accounts` and ensure `state.config.active_account_id` points to the first standalone `device_login` account (if any).
    *   Update `persistConfig()` to filter out accounts with `source === "kraken_session"` from the serialized JSON before calling `idbSet` and `self.kraken.kv.set("azure_kv_config", ...)`, guaranteeing that Kraken SSO session accounts are never persisted to disk or shared across users.

#### 17. `/home/gfidelis/Code/kraken-plugins/azure-kv-integration/backend/plugin.wasm` (Modify)

*   **Type / Scope**: Compiled WASM Binary Target
*   **Proposed Changes**:
    *   Recompile `backend/plugin.wasm` using `GOOS=wasip1 GOARCH=wasm go build -ldflags="-s -w" -o plugin.wasm .` so the updated WASM binary includes the `kraken_auth_get_token` host import.

### Verification Plan

*   **Compilation/Build**:
    *   Backend (`kraken-admin-tool`): `cd /home/gfidelis/Code/go/kraken-admin-tool/backend && go build ./...`
    *   Frontend (`kraken-admin-tool`): `cd /home/gfidelis/Code/go/kraken-admin-tool/frontend && npm run build`
    *   Plugin WASM (`azure-kv-integration`): `cd /home/gfidelis/Code/kraken-plugins/azure-kv-integration/backend && GOOS=wasip1 GOARCH=wasm go build -ldflags="-s -w" -o plugin.wasm .`
*   **Unit Tests**:
    *   `cd /home/gfidelis/Code/go/kraken-admin-tool/backend && go test -v ./internal/auth/... ./internal/plugins/...`
*   **Static Analysis & Linting**:
    *   `cd /home/gfidelis/Code/go/kraken-admin-tool/backend && go vet ./...`
*   **Test Coverage Requirement**:
    *   All unit test scenarios in `backend/internal/auth/ms_broker_test.go` (device code initiation, pending new user creation with HTTP 403, active user JWT + broker session creation, scoped token exchange + logout eviction) and `backend/internal/plugins/wasm_runtime_test.go` (`kraken_auth_get_token` host bridge token retrieval) must pass with 0 failures.
*   **E2E/Integration**:
    *   Verify `/api/auth/microsoft/device-code` and `/api/auth/microsoft/poll` routes respond with proper status codes and polling interval semantics, and verify `plugin.wasm` compiles and exports `kraken_auth_get_token` import cleanly.

--------------------------------------------------------------------------------

<!-- Provenance: Indicates initial structural drafting via EGM. -->

*Drafted with EGM*
