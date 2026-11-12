# Kraken Admin Tool

Unified infrastructure control plane for managing Docker daemons, Compose stacks, Kubernetes clusters, and host system packages with strict zero-trust credential security.

## Language

### Host & Infrastructure Management

**Host**:
A physical or virtual machine hosting container daemons, reachable via local socket, TCP/mTLS, or SSH.
_Avoid_: Machine, node, server-instance

**Credential Policy**:
The per-host security setting specifying whether authentication credentials (SSH keys, passwords, passphrases) and privilege escalation secrets (sudo passwords) are securely stored encrypted in the database or prompted on each administrative use.
_Avoid_: Auth mode, save setting

**Prompt Policy**:
A credential policy state where the system never persists credentials to disk or database, requiring transient in-memory submission per operational request.
_Avoid_: Unsaved mode, ephemeral auth

**Encrypted Store Policy**:
A credential policy state where credentials are encrypted using AES-256-GCM with a server master key before persistence and decrypted only in-memory during execution.
_Avoid_: Saved password, remembered auth

**Privilege Escalation**:
The elevation of host privileges via sudo to execute administrative commands such as package inventory and repository updates.
_Avoid_: Root access, admin run

**Package Update**:
A software package installed on a host system for which a newer version is available in upstream package manager repositories (apt, dnf, yum, pacman, apk, zypper).
_Avoid_: System patch, os upgrade

### Workload & Container Orchestration

**Compose Stack**:
A multi-container application defined and deployed as a cohesive Docker Compose project, identified by the `com.docker.compose.project` label.
_Avoid_: Container group, app bundle, docker group

**Compose Service**:
An individual service component defined within a Docker Compose stack, mapping to one or more container instances.
_Avoid_: Stack component, sub-container

**Container**:
An isolated Docker runtime process instance managed individually or as part of a Compose stack.
_Avoid_: Box, virtual process

**Workload**:
An orchestrated application unit, encompassing Docker containers, Compose stacks, and Kubernetes deployments/pods.
_Avoid_: Task, job

### Source Discovery & Image Lifecycle

**Search Directory**:
An administrator-configured directory path on a target host scanned recursively for author-created Dockerfiles and Compose files.
_Avoid_: Scan path, watch folder

**Source Link**:
A reverse-engineered relationship between a Docker image or Compose stack and a host filesystem source file, established through layer alignment and token scoring.
_Avoid_: File match, source bind

**Retain Upgrade**:
An image update that stays within the current major and minor release branch (or base OS security rebuild), remediating CVEs with zero breaking API changes.
_Avoid_: Safe update, patch bump

**Full Upgrade**:
An image update to the latest stable upstream release on Docker Hub matching the distribution variant.
_Avoid_: Major jump, latest pull

### Extensibility & Plugin Architecture

**Plugin**:
A self-contained extension bundle (`.zip` or `.tar.gz`) containing a `plugin.json` manifest, optional frontend Web Worker logic, and optional backend WASM/container runtime executable.
_Avoid_: Addon, extension pack

**Web Worker Sandbox**:
The isolated browser thread in which plugin frontend logic executes without access to the host DOM, cookies, or localStorage authentication tokens.
_Avoid_: IFrame wrapper, script sandbox

**Remote UI Protocol**:
The declarative message protocol where sandboxed Web Workers transmit JSON UI trees rendered natively by the host React application with active theme inheritance.
_Avoid_: Virtual DOM sync, iframe bridge

**WASM Sandbox**:
An in-process WebAssembly runtime powered by pure-Go `wazero` that executes plugin binaries with memory ceilings, execution timeouts, and strictly mediated host functions (`kraken_log`, `kraken_kv_get`, `kraken_kv_set`).
_Avoid_: CGO runner, wasm subprocess

**Plugin Catalog**:
A remote or private HTTP repository exposing an `index.json` registry of discoverable, verifiable plugins for the online store.
_Avoid_: App marketplace, repo mirror

**Container Sidecar**:
A managed, long-running Docker or Kubernetes container service communicating with the Kraken backend exclusively through a short-lived, cryptographically signed, and replay-protected contract (`internal/plugins/sidecarauth`).
_Avoid_: Worker container, backend daemon

**Sidecar Authentication Contract**:
The cryptographic mutual protocol between Kraken and container sidecars, enforcing HMAC-SHA256 request signatures, canonical payload hashing (`KRKN1`), short-lived JWT context claims (30s), anti-replay cache (60s retention, 30s clock skew window), and rejection of legacy unauthenticated headers (`X-Kraken-User`, `X-Kraken-Role`).
_Avoid_: Sidecar token, plugin API key

**Host OS Verification**:
The authentication barrier verifying host OS credentials via PAM/su/sudo before granting access to local Docker daemon sockets (`unix:///var/run/docker.sock`).
_Avoid_: Local bypass, auto-host bind

**Reverse Proxy Gateway**:
The mandatory TLS gateway terminating HTTPS on port 8443 and enforcing 301 redirection with HSTS from HTTP port 8000, isolating internal services.
_Avoid_: Web router, direct port forward

