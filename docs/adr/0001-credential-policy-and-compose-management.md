# 0001: Credential Retention Policy and Compose Stack Management

## Context
Kraken Admin Tool previously automatically persisted all provided SSH and TLS credentials encrypted in PostgreSQL without giving administrators the option to avoid storing secrets. Furthermore, host maintenance lacked OS package update visibility and sudo privilege escalation management, and container orchestration was limited to individual containers rather than multi-container Docker Compose stacks.

## Decisions

1. **Dual Credential Retention Policy (`always_prompt` vs `save_securely`)**:
   - Host connections and sudo elevation will support two explicit policies: `always_prompt` (credentials supplied ephemerally per request via `X-Host-Password`, `X-Sudo-Password`, or payload headers and zeroed in-memory after use) and `save_securely` (credentials encrypted with AES-256-GCM before DB persistence and omitted from JSON serialization with `json:"-"`).
   - First-time host creation and first-time sudo usage will require user selection between these modes.

2. **Package Manager Abstraction and Sudo Elevation**:
   - Host OS package updates will be detected via SSH using non-interactive CLI package manager probes (`apt`, `dnf`, `yum`, `apk`, `pacman`, `zypper`).
   - Sudo execution passes credentials strictly via stdin (`sudo -S -p ''`) rather than command line arguments or shell variables to prevent exposure in process tables (`ps aux`), shell histories, or log files.

3. **Docker Compose Management via Docker Engine API**:
   - Compose stacks will be identified and grouped using Docker standard labels (`com.docker.compose.project`, `com.docker.compose.service`).
   - Stack lifecycle (start, stop, restart, delete, inspect) will be managed via the unified Docker client, enabling stack management uniformly across local UNIX sockets, remote TCP/mTLS, and SSH tunnels without requiring external CLI dependencies on remote hosts.

## Status
Accepted
