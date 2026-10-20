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
