# Stage 8: Intermediate Zoom Out Review Report

* **Target File**: `design_draft.md` (Full File, Lines 1–381)
* **Reviewer**: `egm-zoom-out` (Skeptical Architect)
* **Context**: Evaluates architectural alignment across `zoom_out_report.md`, `comprehension_report.md`, and `critic_report.md`.

---

## Findings & Mitigations Audit

1. **Boundary Alignment of Stage 6 & Stage 7 Mitigations**:
   * **Direct `id_token` Claim Extraction (Comprehension Finding 1)**: Relying directly on `id_token` claim parsing and validation during `PollMicrosoftDeviceLogin` (instead of making an additional outbound call to `https://graph.microsoft.com/oidc/userinfo` which would require a separate Graph-scoped token exchange) simplifies the login path and eliminates an extra network round-trip on every sign-in.
   * **Per-Session Synchronization & Opportunistic Expiry Sweep (Design Finding D-2)**: Keeping a lightweight per-session mutex on `MicrosoftSessionEntry` and sweeping expired sessions inside `CreateSession` avoids introducing background goroutines or external caches while preventing global lock contention during Microsoft token refreshes.
   * **Removal of `auth.get_session_token` in `adapter.go` (Zoom Out Finding ZO-1)**: Removing the unused host adapter operation preserves the single-interface principle (`kraken_auth_get_token` WASM host import) and prevents accidental token exposure to HTTP callers.
