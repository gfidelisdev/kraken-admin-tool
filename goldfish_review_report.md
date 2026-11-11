# Goldfish Annotated Review Report

**Target:** `Kraken Admin Tool (Host Security, Networking/CORS & UI Responsive Layout)` (`Full System`)

## Prioritized Findings & AI Suggestions

### [Issue 1] [`Host Security & OS Credential Verification`] - Severity: Blocker
*   **Affected Scope:** [`servers/page.tsx`](frontend/src/app/(dashboard)/servers/page.tsx#L2133-L2150) & [`service.go`](backend/internal/docker/service.go#L765-L815)
*   **Discrepancy Description:** Ao selecionar a opção de adicionar "Host Local" (`unix:///var/run/docker.sock`), o sistema permite a criação do servidor sem exigir qualquer credencial (usuário e senha do sistema operacional hospedeiro). Qualquer usuário autenticado no Kraken poderia adicionar o host local e obter privilégios administrativos sobre o daemon Docker e namespace do hospedeiro.
*   **Rationale:** O acesso ao Docker local e execução via `nsenter` no host conferem controle total sobre o servidor hospedeiro. É indispensável exigir autenticação com usuário e senha válidos do sistema operacional antes de vincular e autorizar a gestão do host local.
*   ***AI Suggestion:*** PROPOSE FIX
*   ***Justification:*** Adicionar campos de formulário obrigatórios para Usuário do Hospedeiro e Senha do Usuário/Sudo ao selecionar Host Local no frontend. No backend, validar os campos e executar verificação de autenticação no hospedeiro via PAM/su/sudo através do `LocalExecutor`. Caso as credenciais sejam inválidas, a adição do host é rejeitada.
*   ***Proposed Edit:***
    *   Location: `frontend/src/app/(dashboard)/servers/page.tsx`
    *   Change: Exibir campos obrigatórios de Usuário do SO e Senha para conexão local (`connectionType === "local"`).
    *   Location: `backend/internal/docker/service.go` & `backend/internal/docker/diagnostics.go`
    *   Change: Exigir credenciais para hosts `unix://` e verificar a autenticidade das credenciais contra o sistema hospedeiro antes de salvar ou testar a conexão.

---

### [Issue 2] [`Network, Dynamic API Resolution & CORS on LAN Access`] - Severity: Blocker
*   **Affected Scope:** [`api.ts`](frontend/src/lib/api.ts#L93-L103), [`main.go`](backend/cmd/server/main.go#L162-L172) & [`stream.go`](backend/internal/websocket/stream.go#L35-L50)
*   **Discrepancy Description:** Ao acessar o Kraken através do endereço IP do equipamento (ex: `http://10.4.1.X:3000`), as requisições para a API e o fluxo de login Microsoft falham com erro de CORS (`Cross-Origin Request Blocked: Access-Control-Allow-Origin missing`). O frontend tenta acessar `http://localhost:8080` (que é resolvido localmente na máquina do cliente em vez do servidor) e o backend rejeita origens diferentes de `localhost:3000`.
*   **Rationale:** Usuários corporativos e administradores acessam o Kraken a partir de IPs de rede local ou nomes de rede. O frontend deve resolver a URL da API dinamicamente com base no hostname do navegador (`window.location.hostname:8080`), e o backend deve permitir origens provenientes da porta do frontend (3000/8080) em qualquer IP de rede local.
*   ***AI Suggestion:*** PROPOSE FIX
*   ***Justification:*** Atualizar `getBaseApiUrl()` no frontend para utilizar o hostname ativo do navegador se `NEXT_PUBLIC_API_URL` for `localhost` ou omitido. No backend, adicionar função `AllowOriginFunc` no middleware de CORS do Chi e no Upgrader do WebSocket para aceitar requisições de portas 3000 e 8080 de origens de rede.
*   ***Proposed Edit:***
    *   Location: `frontend/src/lib/api.ts`
    *   Change: Inspecionar `window.location.hostname` dinamicamente quando acessado fora de `localhost`.
    *   Location: `backend/cmd/server/main.go` & `backend/internal/websocket/stream.go`
    *   Change: Configurar `AllowOriginFunc` no CORS e no WebSocket para permitir acessos à porta 3000 a partir de IPs de rede.

---

### [Issue 3] [`Hosts Table Layout & Horizontal Scroll Elimination`] - Severity: Major
*   **Affected Scope:** [`layout.tsx`](frontend/src/app/(dashboard)/layout.tsx#L71) & [`servers/page.tsx`](frontend/src/app/(dashboard)/servers/page.tsx#L1335-L1670)
*   **Discrepancy Description:** A tabela que lista os hosts possui muitas colunas e botões de ação extensos, somado à restrição de largura `max-w-7xl` no layout principal. Isso obriga a barra de rolagem horizontal (scroll lateral) em telas padrão e laptops.
*   **Rationale:** Uma visão operacional de hosts deve apresentar informações essenciais de forma compacta e direta, evitando rolagem horizontal desnecessária.
*   ***AI Suggestion:*** PROPOSE FIX
*   ***Justification:*** Aumentar o `max-w` do layout principal para `max-w-[1600px]`, aplicar truncamento com tooltip na coluna de URL de conexão, compactar os botões de ação e otimizar o espaçamento entre as células da tabela de hosts.
*   ***Proposed Edit:***
    *   Location: `frontend/src/app/(dashboard)/layout.tsx`
    *   Change: Alterar container para `max-w-[1600px] w-full mx-auto`.
    *   Location: `frontend/src/app/(dashboard)/servers/page.tsx`
    *   Change: Truncar URLs longas com tooltip, compactar botões de ações operacionais e reduzir padding excessivo.
