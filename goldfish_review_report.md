Status: resolved

# Goldfish Annotated Review Report

**Target:** `Kraken Admin Tool (Kubernetes Deployment, Transparent Container-to-Pod Plugin Runtime & First-Time Setup)` (`Full System`)

## Prioritized Findings & AI Suggestions

### [Issue 1] [`Kubernetes Native Deployment & Docker Compose Backwards Compatibility`] - Severity: Blocker
*   **Affected Scope:** [`k8s/`](k8s/kustomization.yaml) & [`docker-compose.yml`](docker-compose.yml)
*   **Discrepancy Description:** O Kraken Admin Tool era distribuído apenas via `docker-compose.yml`, sem manifestos Kubernetes nativos (Namespace, RBAC, ConfigMap/Secret, PostgreSQL PVC/Deployment, Backend, Frontend e Nginx TLS Proxy).
*   **Rationale:** Ambientes corporativos e clusters Kubernetes exigem manifestos declarativos completos (`kubectl apply -f k8s/` ou `kubectl apply -k k8s/`) mantendo 100% de compatibilidade retroativa com implantações baseadas em Docker Compose.
*   ***AI Suggestion:*** PROPOSE FIX
*   ***Justification:*** Adicionar o conjunto completo de manifestos Kubernetes em `k8s/` (`00-namespace.yaml` até `06-proxy.yaml` + `kustomization.yaml`) preservando intactos `docker-compose.yml` e `.env.example`, além de auto-gerar e persistir segredos criptográficos no primeiro boot quando não definidos previamente no Secret/env.
*   ***Proposed Edit:***
    *   Location: `k8s/`
    *   Change: Criar manifestos de Namespace, ServiceAccount/ClusterRole/ClusterRoleBinding, ConfigMap/Secret/Nginx template, PostgreSQL stateful PVC/Deployment/Service, Backend Deployment/Service, Frontend Deployment/Service, Proxy Nginx com initContainer de certificado TLS e Kustomization.

---

### [Issue 2] [`Transparent Container-to-Pod Plugin Runtime Conversion in Kubernetes`] - Severity: Blocker
*   **Affected Scope:** [`container_runtime.go`](backend/internal/plugins/container_runtime.go), [`k8s_pod_runtime.go`](backend/internal/plugins/k8s_pod_runtime.go) & [`manager.go`](backend/internal/plugins/manager.go)
*   **Discrepancy Description:** Plugins com runtime `container` (ou `docker` / com `Dockerfile` próprio) dependiam exclusivamente do daemon Docker (`ContainerRuntimeManager`), falhando ou ficando incompatíveis ao executar o Kraken dentro de um cluster Kubernetes sem acesso direto a contêineres Docker lado a lado na mesma rede bridge.
*   **Rationale:** Quando o Kraken opera em Kubernetes, plugins baseados em contêiner devem ser convertidos e inicializados automaticamente como Kubernetes Pods + Services de forma totalmente invisível ao usuário, mantendo o contrato criptográfico `sidecarauth` (`KRKN1` HMAC-SHA256 e JWT efêmero).
*   ***AI Suggestion:*** PROPOSE FIX
*   ***Justification:*** Detectar automaticamente o modo Kubernetes (`KRAKEN_RUNTIME_MODE`, `KUBERNETES_SERVICE_HOST`, `InClusterConfig` ou ausência de daemon Docker com cliente K8s ativo), normalizar `"docker"` para `"container"` e promover plugins com `Dockerfile`, e orquestrar Pods + Services dedicados via `client-go` (incluindo conversão automática de `Dockerfile` multi-stage/runtime em InitContainers/Containers com contexto em Secret quando não houver daemon Docker para build).
*   ***Proposed Edit:***
    *   Location: `backend/internal/plugins/k8s_pod_runtime.go` & `backend/internal/plugins/container_runtime.go`
    *   Change: Implementar `StartContainer`, `StopContainer`, `ExecuteCall`, notificações de ciclo de vida e `PrepareKubernetesPluginBuild` com backend transparente em Kubernetes Pods & Services.

---

### [Issue 3] [`First-Time Initialization & Mandatory Admin Setup Screen`] - Severity: Major
*   **Affected Scope:** [`handler.go`](backend/internal/auth/handler.go), [`AuthContext.tsx`](frontend/src/context/AuthContext.tsx) & [`setup/page.tsx`](frontend/src/app/setup/page.tsx)
*   **Discrepancy Description:** Na primeira inicialização sem `INITIAL_ADMIN_PASSWORD`, o sistema dependia de o operador navegar manualmente até `/register` ou usar credenciais padrão, sem apresentar uma tela dedicada de configuração inicial para definir a senha do administrador e parâmetros essenciais de segurança.
*   **Rationale:** Uma tela dedicada de configuração inicial (`/setup`) garante que no primeiro acesso do sistema o administrador defina obrigatoriamente sua senha e revise configurações essenciais (auto-registro e SSO opcional) antes de liberar o painel ou o fluxo de login.
*   ***AI Suggestion:*** PROPOSE FIX
*   ***Justification:*** Expor endpoints públicos `GET /api/auth/setup-status` e `POST /api/auth/setup` protegidos para execução única (enquanto não houver administrador ativo no banco), e redirecionar automaticamente qualquer acesso inicial no frontend (`/`, `/login`, `/register`) para `/setup`.
*   ***Proposed Edit:***
    *   Location: `backend/internal/auth/handler.go` & `frontend/src/app/setup/page.tsx`
    *   Change: Implementar endpoints de status/configuração inicial e tela interativa `/setup` integrada ao `AuthContext`.

---

### [Issue 4] [`Dynamic Multi-Domain FQDN, Kubernetes Ingress (Port 443) & Automatic TLS Certificate Rollout`] - Severity: Blocker
*   **Affected Scope:** [`network.go`](backend/internal/system/network.go), [`k8s/07-ingress.yaml`](k8s/07-ingress.yaml), [`k8s/01-rbac.yaml`](k8s/01-rbac.yaml) & [`settings/page.tsx`](frontend/src/app/(dashboard)/settings/page.tsx)
*   **Discrepancy Description:** Ao iniciar em Kubernetes via `ip:port` (`NodePort`), administradores precisam posteriormente apontar um ou múltiplos FQDNs corporativos (ex: `kat.mycompany.com`) na porta padrão HTTPS (`443`) via Kubernetes Ingress, exigindo atualização automática do `Ingress`, `ConfigMap`, `Secret` TLS multi-SAN e reinicialização coordenada dos workloads.
*   **Rationale:** A configuração dinâmica de múltiplos nomes DNS em tempo de execução garante transição transparente de `ip:port` para `https://<fqdn>` (443) sem edição manual de manifestos YAML nem perda de compatibilidade com Docker Compose.
*   ***AI Suggestion:*** PROPOSE FIX
*   ***Justification:*** Implementar persistência de configuração de rede (`SystemNetworkConfig`), geração automática de certificado TLS multi-SAN cobrindo todos os FQDNs configurados, reconciliação automática via `client-go` (`kraken-tls-secret`, `kraken-config`, `kraken-nginx-template`, `kraken-ingress`, `Service/proxy`, rollout restart de `proxy`/`frontend`/`backend`), e tela administrativa `/settings` (além de opção no `/setup`).
*   ***Proposed Edit:***
    *   Location: `backend/internal/system/network.go`, `k8s/`, `frontend/src/app/(dashboard)/settings/page.tsx`
    *   Change: Adicionar reconciliador Kubernetes de Ingress/TLS multi-domínio, endpoints `/api/system/network-config` e interface administrativa para gerenciar múltiplos FQDNs e aplicar rollout restart automático.
