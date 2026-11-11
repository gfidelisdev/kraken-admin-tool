# Stage 7: Critic Review Report (`FeatureRange`: Lines 1–171)

* **Target File**: `design_draft.md` (Lines 1–171)
* **Reviewers**: `egm-design` (Hostile Design Critic), `egm-structural` (Structural Validator), `egm-style` (Technical Copy Editor)
* **Standard**: `feature-spec-standard.md`

---

## 1. Hostile Design Critique (`egm-design`)

### Finding D-1: Masking RFC 8628 `slow_down` as `pending_authorization` Causes Upstream Throttling and Collides with `authRateLimiter`
* **Severity**: Major
* **Affected Scope**: `design_draft.md` lines 110–112, 164
* **Finding & Recommendation**:
  1. Line 111 states: *"While `error == "authorization_pending"` or `"slow_down"`, returns HTTP 200 with `{"status": "pending_authorization"}` so the frontend continues polling cleanly."* Per RFC 8628 §3.5, receiving `"slow_down"` from the OAuth2 authorization server requires the client to increase its polling interval by at least 5 seconds for all subsequent requests. Collapsing `"slow_down"` into `"pending_authorization"` hides the backoff signal from the frontend, causing the client to keep polling at the original interval until Microsoft Entra ID invalidates the `device_code` or throttles the backend IP.
  2. Line 164 states that `/api/auth/microsoft/device-code` and `/api/auth/microsoft/poll` are protected by `authRateLimiter`. In `backend/cmd/server/main.go`, `authRateLimiter` allows only 1 request per 2 seconds (burst 5) shared across all `/api/auth/*` endpoints per IP. Multiple operators behind the same NAT polling simultaneously can trip HTTP 429 `Too many login attempts` mid-flow.
  * **Recommendation**: Return distinct status `"slow_down"` (or an updated `interval` field) in the poll response so the frontend increases its polling delay by +5 seconds, and enforce rate limiting tuned for polling (or per-device-code interval enforcement).

### Finding D-2: Lock Contention and Race Conditions During Concurrent Scoped Token Exchanges in `MicrosoftSessionBroker`
* **Severity**: Major
* **Affected Scope**: `design_draft.md` lines 121–124
* **Finding & Recommendation**:
  Lines 121–123 specify that `MicrosoftSessionBroker` uses a `sync.RWMutex` and performs an outbound HTTP `POST` (`grant_type=refresh_token`) to `login.microsoftonline.com` inside `GetScopedToken(ctx, sessionID, scope)`. Holding a broker-wide write lock during network I/O blocks all sessions; releasing the lock without per-session synchronization causes concurrent refresh token exchanges when a plugin requests Key Vault and ARM scopes simultaneously on page load. Additionally, session eviction when users close the browser without calling `/api/auth/logout` must be specified.
  * **Recommendation**: Do not hold the global map lock during outbound HTTP calls (use per-session locking or copy the refresh token under read lock and synchronize updates per session), and sweep expired sessions opportunistically during `CreateSession`.

### Finding D-3: Unaddressed Failure Mode When Backend Restarts During an Active 24-Hour JWT Session
* **Severity**: Major
* **Affected Scope**: `design_draft.md` lines 119–125, 159–169
* **Finding & Recommendation**:
  Because `MicrosoftSessionBroker` is strictly in-memory while Kraken JWTs are valid for 24 hours, a backend restart (or multi-replica deployment) leaves the JWT valid with `auth_provider = "microsoft"`, while `MicrosoftSessionBroker` has no entry for `jti`. This failure mode is handled in DetailedRange line 243 (`Me` downgrades `AuthProvider` to `"local"`), but is omitted from the Feature Specification (`1..171`).
  * **Recommendation**: Document the single-instance in-memory constraint and `/api/auth/me` downgrade behavior explicitly in the Feature Specification (`Technical Plan` / `Non-Functional Requirements`).

### Finding D-4: Email-Only Identity Matching With `tenant = "common"` and First-User Bootstrap
* **Severity**: Major
* **Affected Scope**: `design_draft.md` lines 108–117, 161–164
* **Finding & Recommendation**:
  1. Defaulting `MICROSOFT_TENANT_ID` to `"common"` accepts tokens from any Entra tenant or personal Microsoft account. Matching existing `active` users by email without tenant pinning or verified email checks should be explicitly called out in Security & Risks (recommending `MICROSOFT_TENANT_ID` pinning in multi-user production environments).
  2. Clarify that initial administrator bootstrap uses the seeded local admin account (`admin`), so new unrecognized Microsoft accounts consistently start as `pending` (`viewer`).

---

## 2. Structural Validation (`egm-structural`)

### Finding S-1: Missing Explicit `Risks & Limits` Coverage in `## Non-Functional Requirements`
* **Severity**: Major
* **Affected Scope**: `design_draft.md` lines 159–170 (`## Non-Functional Requirements`)
* **Discrepancy Description & Rationale**: `feature-spec-standard.md` requires explicit coverage of **Risks & Limits** (failure modes, security implications, scalability limits, and mitigations) within the Feature Specification (`1..171`). Adding a `Risks & Limits` bullet under `## Non-Functional Requirements` covering backend restart state loss, single-instance memory constraints, and `MICROSOFT_TENANT_ID` tenant-pinning guidance resolves this gap.

### Finding S-2: Unlinked Stakeholder Attribution in Alternative C Rejection Rationale
* **Severity**: Nit
* **Affected Scope**: `design_draft.md` line 155 (`### Alternative C`)
* **Discrepancy Description & Rationale**: Replace `"User explicitly required..."` with the direct technical/security scope trade-off (`"Rejected by author: Persisting interactive user refresh tokens on disk expands the credential blast radius beyond the active operator session and duplicates existing unattended authentication mechanisms (Service Principal and Managed Identity)."`).

---

## 3. Style Review (`egm-style` — Lines 1–171)

### Finding ST-1: Hyperbole (`"first-class"`)
* **Severity**: Nit
* **Affected Scope**: `design_draft.md` line 13
* **Rationale**: Replace `"introduces first-class Microsoft OAuth2 v2.0 Device Code Flow authentication"` with `"adds Microsoft OAuth2 v2.0 Device Code Flow authentication"`.

### Finding ST-2: Vague Adverb (`"cleanly"`)
* **Severity**: Nit
* **Affected Scope**: `design_draft.md` line 111
* **Rationale**: Replace `"so the frontend continues polling cleanly"` with `"so the frontend continues polling without treating the response as a failed HTTP request"`.

### Finding ST-3: Synonym Redundancy (`"wipes and deletes"`)
* **Severity**: Nit
* **Affected Scope**: `design_draft.md` line 124
* **Rationale**: Replace `"wipes and deletes the session entry"` with `"clears token fields and deletes the session entry"`.

### Finding ST-4: Uncalibrated Claim in `Rollout & Rollback`
* **Severity**: Nit
* **Affected Scope**: `design_draft.md` line 168
* **Rationale**: Replace `"Rolling back is a clean binary revert with zero database schema incompatibility"` with `"Rolling back requires only reverting the backend and frontend binaries; no database schema rollback is required."`
