# Microsoft Device Code Authentication & Plugin Token Broker

* **Author:** Guilherme Fidelis da Silva
* **Date:** 2026-11-09

## Overview

Kraken Admin Tool currently authenticates users exclusively via local username and password credentials stored in its database, while plugins that integrate with Microsoft Azure (such as `com.kraken.azure-keyvault`) must run their own standalone Microsoft OAuth2 Device Code Flow inside the plugin UI. This feature adds first-class Microsoft OAuth2 v2.0 Device Code Flow authentication directly to the Kraken Admin Tool login screen (`LoginPage`).

When an operator signs in to Kraken with a Microsoft account, Kraken validates or provisions the user record (creating new accounts with `pending` status for administrator approval, or matching existing `active` accounts by email) and binds the resulting Microsoft OAuth2 tokens strictly to that user's active Kraken backend session. During that session, Kraken acts as a backend Token Broker: it exposes only non-sensitive Microsoft account metadata (`email`, `name`, `tenant_id`, `source: "kraken_session"`) to plugin frontend workers, and issues scoped access tokens (such as `https://vault.azure.net/.default` or `https://management.azure.com/.default`) on demand to authorized plugin backends via the WASM/host bridge. When an operator signs in to Kraken with a local account, no `kraken_session` token is present, and plugins automatically fall back to their own standalone authentication flow.

## Analysis

* **Contradiction 1 — Multi-Resource OAuth2 Scopes vs. Single Login Prompt**: Microsoft Entra ID v2.0 issues an `access_token` for only a single resource audience per token request, yet plugins like `com.kraken.azure-keyvault` require tokens for both Azure Key Vault (`https://vault.azure.net/.default`) and Azure Resource Manager (`https://management.azure.com/.default`). Resolving this without exposing the long-lived `refresh_token` to untrusted browser Web Workers requires the Kraken Go backend to hold the `refresh_token` in a session-scoped store and broker resource-specific `access_token`s on demand via the host bridge (`kraken_auth_get_token` / host adapter).
* **Contradiction 2 — Public Device Code Client ID vs. Restricted Admin Control Plane Access**: Using the public Azure CLI `client_id` (`04b07795-8ddb-461a-bbee-02f9e1bf7b46`) and `common` tenant allows any Microsoft account holder to complete the OAuth2 handshake. To prevent unauthorized access to Kraken's Docker and Kubernetes control plane, unaligned Microsoft accounts are created with `status = "pending"` and blocked at login until an administrator approves them and assigns a role (`admin`, `operator`, or `viewer`), matching the existing self-registration governance in `backend/internal/auth/handler.go`.
* **Contradiction 3 — Shared Plugin KV Storage vs. Per-User SSO Isolation**: `PluginKVStore` is keyed only by `(plugin_id, key)`. Persisting a user's Kraken SSO Microsoft tokens into `azure_kv_config` would leak one operator's Microsoft credentials to other local users on the same Kraken instance. Keeping `kraken_session` tokens strictly ephemeral in the user's backend session (tied to their JWT `SessionID` / `UserID`) and never writing them into `PluginKVStore` eliminates cross-user credential bleed while preserving manually connected plugin accounts for local logins.

## Goals vs. Non-Goals

* **In-Scope Goals:**
  * Add Microsoft OAuth2 v2.0 Device Code Flow initiation and polling endpoints to the Kraken auth backend (`/api/auth/microsoft/device-code` and `/api/auth/microsoft/poll`) and UI controls on `frontend/src/app/login/page.tsx`.
  * Match authenticated Microsoft accounts by email to existing `models.User` records, or auto-provision new users with `status: "pending"` and `role: "viewer"` requiring admin approval before issuing a Kraken JWT.
  * Store Microsoft OAuth2 tokens (`access_token`, `refresh_token`, `tenant_id`, `expires_at`) in an ephemeral, session-bound backend store tied to the authenticated Kraken JWT session (cleared on logout or JWT expiration).
  * Expose non-sensitive Microsoft session metadata (`auth_provider`, `microsoft_account` with `email`, `name`, `tenant_id`, `source: "kraken_session"`) to the frontend `AuthContext` and plugin worker SDK (`self.kraken.user` / `self.kraken.session`).
  * Provide a permission-gated host bridge mechanism (`kraken_auth_get_token` WASM import and host adapter action) so authorized plugins (`com.kraken.azure-keyvault`) can obtain scoped Microsoft access tokens during an active Microsoft user session, and fall back to plugin-managed credentials when logged in as a local user.
* **Out-of-Scope Non-Goals:**
  * Browser redirect-based OAuth2 Authorization Code Flow requiring public callback URLs or mandatory custom Entra App Registration secrets.
  * Persisting Kraken-level Microsoft SSO `refresh_token`s across user logouts or writing them into global `PluginKVStore` records for unattended background jobs.
  * Replacing existing local username/password + TOTP authentication.

## Alternatives Weighed

* **Alternative 1: Inject raw Microsoft `access_token` and `refresh_token` directly into `self.kraken` in the plugin Web Worker and save into `PluginKVStore` (`azure_kv_config`).**
  * *Rejected by author:* Exposes long-lived refresh tokens to browser Web Workers (vulnerable to XSS or rogue plugin scripts) and contaminates the global `PluginKVStore` shared across all Kraken users, violating session isolation.
* **Alternative 2: Auto-approve all Microsoft Device Code logins as active users.**
  * *Rejected by author:* Because the default public client ID (`04b07795-8ddb-461a-bbee-02f9e1bf7b46`) with `tenant=common` accepts any Microsoft account globally, auto-activating accounts would allow unauthorized external users into the Kraken control plane. New accounts must start as `pending`.
* **Alternative 3: Persist Kraken Microsoft SSO tokens permanently in the database for all users.**
  * *Rejected by author:* The user explicitly required that Kraken-level Microsoft credentials remain valid only for the duration of the authenticated session.

## Key Decisions & Staging for Next Steps

* **Provisioning Policy:** Unrecognized Microsoft emails create a `models.User` with `Status: "pending"` (blocking login with HTTP 403 Pending Approval until an admin approves); existing `active` users with matching email log in immediately.
* **Security Architecture:** Backend Token Broker pattern. Tokens stay in the Kraken backend bound to the user's active session; plugins receive public metadata in the worker UI and request scoped access tokens via the backend WASM/host bridge.
* **Fallback Behavior:** Local Kraken sessions have no `kraken_session` token in the broker, so `azure-kv-integration` and other plugins fall back to their existing standalone Device Code or Service Principal configuration.
