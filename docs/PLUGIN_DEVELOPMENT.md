# Guia de Desenvolvimento de Plugins — Kraken Admin Tool

Este guia detalha o processo completo de desenvolvimento, empacotamento, assinatura e distribuição de plugins para o **Kraken Admin Tool**.

---

## 1. Visão Geral da Arquitetura

O ecossistema de plugins do Kraken foi projetado com isolamento de segurança em múltiplas camadas:

```
┌────────────────────────────────────────────────────────────────────────┐
│                        KRAKEN ADMIN TOOL HOST                          │
│                                                                        │
│  ┌───────────────────────┐              ┌───────────────────────────┐  │
│  │ Host React UI         │ ◄─── RPC ───►│ Web Worker Sandbox        │  │
│  │ (Tailwind & Themes:   │  postMessage │ (worker.js isolado)       │  │
│  │  Dark, Light,         │              │ Sem acesso a DOM/Cookies/ │  │
│  │  HighContrast, Sepia) │              │ LocalStorage (Auth Token) │  │
│  └───────────────────────┘              └─────────────┬─────────────┘  │
│             │                                         │                │
│             │ REST API                                │ backend.call() │
│             ▼                                         ▼                │
│  ┌──────────────────────────────────────────────────────────────────┐  │
│  │ Kraken Go Backend (Plugin Manager & Router)                      │  │
│  │                                                                  │  │
│  │  ┌─────────────────────────────┐   ┌──────────────────────────┐  │  │
│  │  │ WASM Runtime (wazero)       │   │ Container Sidecar        │  │  │
│  │  │ In-Process Sandbox          │   │ Dedicated Docker network │  │  │
│  │  │ Memory limit, timeouts,     │   │ Read-only rootfs, quotas │  │  │
│  │  │ Scoped KV DB & Audit Logs   │   │                          │  │  │
│  │  └─────────────────────────────┘   └──────────────────────────┘  │  │
│  └──────────────────────────────────────────────────────────────────┘  │
└────────────────────────────────────────────────────────────────────────┘
```

### Isolamento de Interface Sem IFrames
- O código JavaScript do plugin roda dentro de um **Web Worker dedicado**.
- O Worker **não tem acesso** ao DOM (`window`, `document`), **não tem acesso** a cookies e **não tem acesso** ao `localStorage` (onde o JWT de autenticação reside).
- A interface é declarativa: o Worker emite árvores JSON via `kraken.render(...)` e o Kraken renderiza nativamente na aplicação host.
- A interface herda automaticamente todas as variáveis CSS do tema ativo no Kraken (Escuro, Claro, Alto Contraste, Seguro para Daltônicos, Sépia/Noturno).
- Para componentes com HTML/SVG estilizado personalizado, o Kraken utiliza um **Closed Shadow DOM** sanitizado.

---

## 2. Estrutura de Arquivos do Plugin

Um pacote de plugin (.zip ou .tar.gz) deve ter a seguinte organização interna:

```text
meu-plugin/
├── plugin.json             # (Obrigatório) Manifesto com metadados e permissões
├── README.md               # (Recomendado) Documentação em Markdown exibida na loja
├── icon.svg                # (Opcional) Ícone vetorial do plugin
├── frontend/
│   └── worker.js           # (Opcional) Lógica de interface executada no Web Worker
└── backend/
    └── plugin.wasm         # (Opcional) Binário compilado para WebAssembly (WASIP1)
```

---

## 3. Especificação do Manifesto (`plugin.json`)

O arquivo `plugin.json` na raiz define a identidade, pontos de extensão e permissões necessárias:

```json
{
  "id": "com.minhaempresa.meu-plugin",
  "name": "Meu Plugin Kraken",
  "version": "1.0.0",
  "description": "Descrição detalhada do plugin e de sua utilidade administrativa",
  "author": "Minha Empresa / Desenvolvedor",
  "homepage": "https://exemplo.com/plugin",
  "license": "Apache-2.0",
  "min_kraken_version": "1.0.0",
  "runtime": {
    "type": "wasm",
    "entrypoint": "backend/plugin.wasm"
  },
  "frontend": {
    "entrypoint": "frontend/worker.js"
  },
  "permissions": [
    "ui:sidebar",
    "ui:dashboard_widget",
    "docker:read",
    "storage:kv"
  ],
  "allowed_hosts": [
    "api.meuservico.com"
  ],
  "ui_extensions": {
    "sidebar": [
      {
        "id": "meu-plugin-nav",
        "title": "Meu Plugin",
        "icon": "shield-check",
        "badge": "Novo"
      }
    ],
    "dashboard_widgets": [
      {
        "id": "meu-plugin-widget",
        "title": "Visão do Meu Plugin"
      }
    ],
    "server_tabs": [
      {
        "id": "meu-plugin-server-tab",
        "title": "Aba do Host"
      }
    ]
  }
}
```

### Permissões Suportadas

| Permissão | Finalidade |
| :--- | :--- |
| `ui:sidebar` | Adiciona um item na barra lateral esquerda |
| `ui:dashboard_widget` | Exibe um widget no painel principal (Overview) |
| `ui:server_tab` | Adiciona aba nos detalhes dos servidores gerenciados |
| `docker:read` | Consulta containers, imagens, volumes e stacks |
| `docker:write` | Inicia, para ou gerencia containers e stacks |
| `hosts:read` | Consulta inventário e atualizações de hosts SSH |
| `hosts:exec` | Executa comandos no host |
| `k8s:read` | Consulta Pods e Deployments de clusters Kubernetes |
| `k8s:write` | Reinicia ou escala workloads Kubernetes |
| `storage:kv` | Armazena chaves e valores isolados no banco de dados do Kraken |
| `http:outbound` | Permite chamadas HTTP externas para os domínios em `allowed_hosts` |

---

## 4. Desenvolvendo a Interface (`frontend/worker.js`)

No Web Worker, o objeto global `kraken` é automaticamente injetado pelo Host SDK:

### API Global `kraken`

```javascript
// Renderizar a árvore declarativa de interface
kraken.render(uiNode);

// Escutar eventos de cliques em botões ou formulários
kraken.on(actionId, (payload, eventType) => { ... });

// Invocar ações no backend (WASM ou Container)
const result = await kraken.backend.call(actionName, payloadObject);

// Gravar e recuperar dados no KV Store isolado
await kraken.kv.set("chave", "valor");
const valor = await kraken.kv.get("chave");

// Registrar mensagens no console e log de auditoria
kraken.log("Mensagem de log");
```

### Componentes Declarativos Suportados

A árvore emitida para `kraken.render()` aceita os seguintes nós:

1. **`Page`**: Contêiner raiz com título e descrição.
2. **`Grid`**: Grade responsiva (1 a 4 colunas).
3. **`Section` / `Card`**: Cartão com título, descrição e lista de filhos.
4. **`StatCard`**: Cartão de métrica com valor destacado, rótulo e badge (`running`, `stopped`, `error`, `pending`, `docker`, `k8s`, `outline`).
5. **`Table`**: Tabela de dados com colunas `{ key, label }` e linhas `{ ... }`.
6. **`Button`**: Botão de ação com `actionId` ou `id` capturável em `kraken.on()`.
7. **`Badge`**: Etiqueta colorida de status.
8. **`Alert`**: Caixa de alerta destacada (`info`, `warning`, `destructive`).
9. **`Terminal`**: Bloco de saída monoespaçado simulando terminal de logs.
10. **`CustomShadowHtml`**: HTML/CSS renderizado em Shadow DOM fechado.

### Exemplo Completo de `worker.js`

```javascript
let currentCount = 0;

function updateUI() {
  kraken.render({
    type: "Page",
    title: "Monitor de Processos",
    description: "Plugin de exemplo executando em Web Worker",
    children: [
      {
        type: "Grid",
        children: [
          {
            type: "StatCard",
            title: "Requisições Processadas",
            value: String(currentCount),
            variant: "running",
            label: "Ativo"
          }
        ]
      },
      {
        type: "Card",
        title: "Ações",
        children: [
          {
            type: "Button",
            id: "btn-incrementar",
            title: "Processar Nova Tarefa",
            variant: "default"
          }
        ]
      }
    ]
  });
}

kraken.on("btn-incrementar", async () => {
  kraken.log("Invocando WASM backend...");
  const res = await kraken.backend.call("processar", { delta: 1 });
  currentCount += (res.incremented_by || 1);
  updateUI();
});

// Renderização inicial
updateUI();
```

---

## 5. Desenvolvendo o Backend WASM (`backend/main.go`)

O runtime WASM do Kraken é baseado em **WASI (WebAssembly System Interface)** via `wazero`.
Plugins podem ser escritos em qualquer linguagem que compile para WASI (`GOOS=wasip1 GOARCH=wasm`, Rust `wasm32-wasip1`, C/C++, TinyGo).

### Exemplo em Go

Crie o arquivo `backend/main.go`:

```go
package main

import (
	"encoding/json"
	"io"
	"os"
)

type Request struct {
	Action   string          `json:"action"`
	Payload  json.RawMessage `json:"payload"`
	PluginID string          `json:"plugin_id"`
}

type Response struct {
	Status        string `json:"status"`
	IncrementedBy int    `json:"incremented_by"`
}

func main() {
	var req Request
	data, _ := io.ReadAll(os.Stdin)
	if len(data) > 0 {
		_ = json.Unmarshal(data, &req)
	}

	// Processa a requisição
	resp := Response{
		Status:        "ok",
		IncrementedBy: 1,
	}

	out, _ := json.Marshal(resp)
	os.Stdout.Write(out)
}
```

### Compilando para WASM:

```bash
GOOS=wasip1 GOARCH=wasm go build -o backend/plugin.wasm backend/main.go
```

---

## 6. Empacotando o Plugin

Gere um arquivo `.zip` ou `.tar.gz` contendo todos os arquivos na raiz do arquivo compactado:

### Usando ZIP:
```bash
zip -r meu-plugin.zip plugin.json README.md frontend/ backend/
```

### Usando TAR.GZ:
```bash
tar -czf meu-plugin.tar.gz plugin.json README.md frontend/ backend/
```

> **Atenção**: O Kraken impõe um limite máximo de **50 MB** para o arquivo de upload e **150 MB** descompactado, com proteção nativa contra *Zip-Slip* e descompressão maliciosa.

---

## 7. Assinatura Digital Criptográfica (Ed25519)

Para garantir autenticidade e integridade em ambientes de produção, o Kraken suporta assinaturas digitais Ed25519.

### 7.1 Compilar o Utilitário de Assinatura

O repositório do Kraken inclui a ferramenta `pluginsign`:

```bash
cd backend
go build -o ../bin/pluginsign ./cmd/pluginsign
```

### 7.2 Gerar um Par de Chaves

```bash
./bin/pluginsign -generate-key
```

Saída:
```text
=== New Ed25519 Plugin Signing Keypair ===
Public Key (register in Kraken): 39c7dfdf54db5f778a6b7afda98b5fcdf7bbe4fa8388a3ce584de43461e7f7a6
Private Key (KEEP SECRET!):      877fefd4b9c9dc1dfca3517516557403f0f175e819f79ae9f70c4448cb6d2360...
```

1. Cadastre a **Chave Pública** no Kraken em **Plugins** > **Gerenciar Chaves Confiáveis**.
2. Guarde a **Chave Privada** com segurança.

### 7.3 Assinar o Pacote do Plugin

```bash
./bin/pluginsign -sign meu-plugin.zip -private-key <CHAVE_PRIVADA_HEX>
```

Isso gera o arquivo `meu-plugin.zip.sig`. Ao hospedar o plugin em um catálogo, sirva a assinatura no mesmo diretório adicionando `.sig` ao final do link de download.

---

## 8. Publicando em um Catálogo Online (Loja de Plugins)

Qualquer servidor web estático (GitHub Pages, AWS S3, Nginx interno) pode atuar como um catálogo de plugins do Kraken. Basta expor um arquivo `index.json`:

```json
{
  "name": "Catálogo de Plugins Internos",
  "description": "Repositório privado de plugins da organização",
  "url": "https://plugins.minhaempresa.com/index.json",
  "updated_at": "2026-10-28T12:00:00Z",
  "plugins": [
    {
      "id": "com.minhaempresa.meu-plugin",
      "name": "Meu Plugin Kraken",
      "version": "1.0.0",
      "description": "Descrição detalhada do plugin",
      "author": "Minha Empresa",
      "homepage": "https://minhaempresa.com",
      "license": "Proprietary",
      "download_url": "https://plugins.minhaempresa.com/downloads/meu-plugin.zip",
      "sha256": "4fd62f351dff28b5d0513fb409bb2eba33a08b1293e9706f5a1cb624588dc313",
      "runtime_type": "wasm",
      "permissions": [
        "ui:sidebar",
        "docker:read"
      ],
      "readme_markdown": "# Meu Plugin\n\nInstruções e documentação..."
    }
  ]
}
```

---

## 9. Instalando no Kraken Admin Tool

Os administradores têm 3 opções fáceis para instalar plugins:

1. **Loja Online (Catálogos)**:
   - Acesse **Plugins** na barra lateral.
   - Na aba **Loja Online**, visualize os plugins disponíveis e clique em **Instalar**.
   - Para adicionar catálogos corporativos, clique em **Gerenciar Catálogos** e informe a URL do `index.json`.

2. **Instalação Direta por URL**:
   - Na aba **Instalar por URL / Arquivo**, cole o link direto para o arquivo `.zip` ou `.tar.gz`.
   - Clique em **Inspecionar e Visualizar**.
   - O Kraken baixará o pacote temporariamente, verificará a assinatura digital e apresentará a lista de permissões solicitadas antes de confirmar a instalação.

3. **Upload de Arquivo Local**:
   - Na aba **Instalar por URL / Arquivo**, arraste ou selecione o arquivo `.zip` ou `.tar.gz` do seu computador.
   - Revise o manifesto e confirme a instalação com um clique.
