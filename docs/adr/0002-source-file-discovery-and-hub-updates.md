# 0002: Source-File Discovery, Reverse-Engineering Mapping, and Docker Hub Image Updates

## Context
Dockerfiles in Kraken Admin Tool were historically reconstructed from scratch via reverse engineering of image layer histories and inspect metadata. While functional, administrators often have author-created Dockerfiles and Docker Compose files deployed across directories on their target hosts.
Furthermore, updating images to remediate upstream Common Vulnerabilities and Exposures (CVEs) required manual terminal pulls and error-prone manual container recreate commands.

## Decisions

1. **Immutable Target Host Source Files & Database Overrides**:
   - Discovered source files on target hosts (via local socket or SSH) are treated as **strictly read-only** (`is_read_only: true`).
   - When administrators view a workload's Dockerfile or Compose YAML, the system retrieves the linked host file.
   - Any user modifications in the UI or API are persisted exclusively into the database tables (`ImageDockerfileConfig` / `ComposeStackConfig`), creating a tracked database override without altering files on the target host filesystem.

2. **Administrator-Configured Search Directories & Heuristics**:
   - Administrators can define search directories per host (`SourceSearchDir`) with configurable recursion, max depth limits, and security path validation (preventing traversal into root or pseudo-filesystems such as `/proc`, `/sys`, `/dev`).
   - The scanner identifies candidates matching standard names (`dockerfile*`, `*.dockerfile`, `docker-compose*.y*ml`, `compose*.y*ml`) as well as heuristic candidates (files starting with directives like `FROM`, `ARG`, `services:`).

3. **Reverse-Engineering Matching Engine & Source Link Persistence**:
   - The system extracts instruction AST tokens from candidate files and correlates them with container image layer history and inspect data (Base image alignment, ENV parity, EXPOSE ports, CMD/ENTRYPOINT, RUN token similarity).
   - Confident matches (score >= 0.85) are automatically established as `SourceLink` records (`status: "auto_matched"`), while lower-confidence candidates allow administrator confirmation or rejection.
   - Compose files are matched against running stack services and declared container images.

4. **On-Demand & Scheduled Scanning**:
   - Scans run on-demand with real-time SSE progress streaming.
   - Administrators can optionally enable automated background scheduled scans per search directory.

5. **Docker Hub Image Updates (Retain vs. Full) & Rollback**:
   - Native integration with Docker Hub v2 API to detect available image updates.
   - Administrators are presented two explicit paths:
     - **Retain Compatibility**: Updates to the latest patch release within the same major/minor line (or base image security rebuild), ensuring zero breaking changes.
     - **Full Upgrade**: Upgrades to the highest stable release on Docker Hub matching the distribution variant (e.g. `-alpine`, `-slim`).
   - Workloads can be automatically recreated in-place with rollback safety: if the newly created container fails to start, the system automatically restores the backup container.

## Status
Accepted
