# Kraken Admin Tool 🐙⚡

**Kraken Admin Tool** is a modern, unified, production-ready full-stack control plane dashboard for managing **Docker** and **Kubernetes (K8s)** infrastructure.

Built with a modular **Go (Chi Router)** backend and a sleek **Next.js 15 (App Router)** frontend with **Tailwind CSS**, **shadcn/ui** design patterns, and **Gorilla WebSockets** for live container and pod log streaming.

---

## 🌟 Features

### 🔐 Authentication & Security
- **Unified HTTPS & HSTS Proxy**: Gateway reverso Nginx com terminação TLS obrigatória na porta **8443**, redirecionamento automático 301 de HTTP (**8000**) para HTTPS, cabeçalhos HSTS (`max-age=31536000`), e isolamento de portas internas (3000 e 8080 restritas à rede interna).
- **Host OS Credential Verification**: Adição e gestão do host Docker local (`unix:///var/run/docker.sock`) exige autenticação obrigatória do sistema operacional hospedeiro via PAM/su/sudo. Conexões sem credenciais ou automáticas são estritamente bloqueadas.
- **Contrato Seguro Kraken ↔ Sidecar (`sidecarauth`)**: Chamadas a containers de plugins utilizam assinatura criptográfica HMAC-SHA256 sobre requisição canônica (`KRKN1`), JWT de contexto efêmero (30s) vinculado à sessão Kraken (`kraken_session_id`), cache anti-replay (60s) e segredos por instalação rotacionáveis (`KRAKEN_PLUGIN_INSTANCE_SECRET`). Cabeçalhos inseguros (`X-Kraken-User` e `X-Kraken-Role`) foram eliminados e são rejeitados.
- **JWT Authentication & TOTP**: Emissão de tokens de acesso seguros com HS256, suporte a TOTP 2FA, recuperação de senha administrativa e autenticação Broker Microsoft SSO (OAuth2 Device Code Flow).
- **Default Seed User**: Inicializa automaticamente o usuário administrador inicial (`admin` / `admin123`) na primeira execução.

### 🐳 Docker Management
- **Container Lifecycle**: List all containers (running, stopped, paused), start, stop, restart, and safely remove containers with confirmation modals.
- **Image Hub**: View local images, inspect repository tags, virtual sizes, creation dates, pull new images from registries, and delete unused images.
- **Real-Time Log Streaming**: Streams Docker stdout/stderr using Docker multiplexing demux (`stdcopy.StdCopy`) via live WebSockets directly into the browser terminal.

### ☸️ Kubernetes (K8s) Management
- **Multi-Namespace Pods**: Inspect pods across any or all namespaces, view status badges (Running, Pending, CrashLoopBackOff, Failed), restart counts, node placements, age, and pod deletion.
- **Deployment Control**: View deployment health (ready vs desired replicas), trigger interactive **Replica Scaling** (+ / -), and initiate **Rollout Restarts** via pod template annotations.
- **Live Pod Log Streaming**: Stream pod container logs in real time via WebSockets with customizable tail line count.
- **Dual-Mode K8s Client**: Automatic fallback between `rest.InClusterConfig()` (when running inside a Kubernetes cluster) and `~/.kube/config` (when running locally or in Docker).

### 🖥️ Interactive Live Log Terminal
- Web console terminal featuring ANSI-inspired syntax highlighting, live auto-scroll with pause toggle, search/filter input, buffer size management, copy-to-clipboard, and clean disconnect handling.

### 📜 Comprehensive Audit Logs
- Automatically logs all critical infrastructure changes (container start/stop, image pull, pod delete, deployment scale) with timestamp, actor username, resource type, payload details, and success/failure status in PostgreSQL.

---

## 🏗️ Architecture

```
kraken-admin-tool/
├── backend/                  # Go 1.24 Backend
│   ├── cmd/server/main.go    # HTTP & WS Server Entrypoint, Routes & Graceful Shutdown
│   ├── internal/
│   │   ├── audit/            # Audit Log Service & Handler
│   │   ├── auth/             # JWT Generation, Verification & Chi Middleware
│   │   ├── config/           # Environment Configuration & Defaults
│   │   ├── database/         # GORM PostgreSQL Connection & Auto-Migration (SQLite fallback)
│   │   ├── docker/           # Official Docker Go SDK Client & Handlers
│   │   ├── k8s/              # Kubernetes Client-Go SDK & Handlers
│   │   ├── models/           # GORM Database Models & API Request/Response DTOs
│   │   ├── system/           # Health & Telemetry Status Handlers
│   │   └── websocket/        # Gorilla WebSocket Log Stream Handlers
│   ├── Dockerfile            # Multi-stage Alpine Production Image
│   ├── go.mod
│   └── go.sum
├── frontend/                 # Next.js 15 App Router Frontend
│   ├── src/
│   │   ├── app/              # Next.js App Router (Dashboard, Containers, Images, Pods, Deployments, Logs, Audit)
│   │   ├── components/       # UI (Cards, Tables, Modals, Buttons, Badges, Tabs) & Layout (Sidebar, Header)
│   │   │   ├── layout/       # Sidebar & Header Navigation
│   │   │   ├── modals/       # PullImageModal, ScaleDeploymentModal, ConfirmActionModal
│   │   │   ├── terminal/     # Interactive LogViewer with WebSocket stream
│   │   │   └── ui/           # Reusable Atomic UI Components (shadcn/ui style)
│   │   ├── context/          # AuthContext (JWT State & LocalStorage Management)
│   │   ├── hooks/            # useWebSocketLogs (Auto-reconnect, pause/resume, buffer)
│   │   ├── lib/              # API Client (Fetch with Bearer interceptor) & Utilities
│   │   └── types/            # TypeScript Interface Definitions
│   ├── Dockerfile            # Multi-stage Standalone Production Image
│   ├── package.json
│   └── tailwind.config.ts    # Custom Dark Mode Theme
├── docker-compose.yml        # Orchestrates PostgreSQL 16, Backend & Frontend
├── .env.example              # Environment Variable Template
└── README.md
```

---

## 🚀 Quick Start with Docker Compose

Ensure **Docker** and **Docker Compose** are installed and running on your host machine.

### 1. Clone & Start
```bash
cd kraken-admin-tool
docker compose up -d --build
```

### 2. Access the Application
- **HTTPS Gateway (Dashboard & API)**: [https://localhost:8443](https://localhost:8443)
- **HTTP Auto-Redirect (Port 8000 -> 8443)**: [http://localhost:8000](http://localhost:8000) (Redirecionamento automático 301 para HTTPS com cabeçalhos HSTS)
- **API Health**: [https://localhost:8443/api/system/status](https://localhost:8443/api/system/status)
- **Isolamento de Rede**: As portas internas `3000` (frontend) e `8080` (backend) operam exclusivamente dentro da rede privada `kraken-network`, protegidas contra conexões externas diretas.

### 3. Default Credentials
| Field | Value |
|---|---|
| **Username** | `admin` |
| **Password** | `admin123` |

*(A button to auto-fill these credentials is available on the login page for convenience).*

---

## 🛠️ Local Development (Without Docker)

You can run both services directly on your host machine:

### Backend (Go)
```bash
cd backend

# Run automated tests
go test ./...

# Start the server (automatically connects to local SQLite if PostgreSQL is not running)
go run cmd/server/main.go
```
The backend will listen on `http://localhost:8080`.

### Frontend (Next.js)
```bash
cd frontend

# Install dependencies
npm install

# Start development server
npm run dev
```
The frontend will start at `http://localhost:3000`.

---

## 📡 API Reference

### Public Routes
- `GET /health` - Healthcheck endpoint
- `POST /api/auth/register` - Create user account
- `POST /api/auth/login` - Authenticate and acquire JWT token

### Protected Routes (`Authorization: Bearer <token>`)
- `GET /api/auth/me` - Current authenticated user profile
- `GET /api/system/status` - Docker engine & K8s cluster connectivity status & resource metrics
- `GET /api/audit-logs` - Query paginated system activity and audit history

#### Docker Endpoints
- `GET /api/docker/containers?all=true` - List containers
- `POST /api/docker/containers/{id}/start` - Start container
- `POST /api/docker/containers/{id}/stop` - Stop container
- `POST /api/docker/containers/{id}/restart` - Restart container
- `DELETE /api/docker/containers/{id}?force=true` - Remove container
- `GET /api/docker/images` - List local images
- `POST /api/docker/images/pull` - Pull image (`{"image": "nginx:latest"}`)
- `DELETE /api/docker/images/{id}` - Remove local image

#### Kubernetes Endpoints
- `GET /api/k8s/pods?namespace={ns}` - List pods (omit `namespace` for all namespaces)
- `DELETE /api/k8s/pods/{namespace}/{name}` - Delete pod
- `GET /api/k8s/deployments?namespace={ns}` - List deployments
- `POST /api/k8s/deployments/{namespace}/{name}/scale` - Scale replicas (`{"replicas": 3}`)
- `POST /api/k8s/deployments/{namespace}/{name}/restart` - Rollout restart deployment

#### WebSocket Endpoints (`?token=<jwt_token>`)
- `ws://localhost:8080/api/ws/docker/logs?containerId={id}&follow=true&tail=100` - Real-time Docker container log stream
- `ws://localhost:8080/api/ws/k8s/logs?namespace={ns}&pod={name}&container={container}&tail=100` - Real-time K8s pod log stream

---

## 🛡️ Production & Kubernetes Deployment Notes

When deploying the Kraken backend container inside a Kubernetes cluster:
1. **In-Cluster Authentication**: The Go backend detects the in-cluster service account token at `/var/run/secrets/kubernetes.io/serviceaccount/` automatically.
2. **RBAC ClusterRole**: Grant the service account permissions to inspect/manage `pods`, `pods/log`, `deployments`, and `namespaces`:
   ```yaml
   apiVersion: rbac.authorization.k8s.io/v1
   kind: ClusterRole
   metadata:
     name: kraken-admin-role
   rules:
   - apiGroups: ["", "apps"]
     resources: ["pods", "pods/log", "deployments", "namespaces"]
     verbs: ["get", "list", "watch", "update", "patch", "delete"]
   ```
3. **Docker Daemon Access**: Mount `/var/run/docker.sock` via `hostPath` volume if container management on node level is required.

---

## 📜 License
MIT License. Built for modern cloud-native operators.
