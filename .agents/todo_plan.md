# Plano Técnico de Implementação — Suporte SSH (Certificado/Chave e Usuário/Senha) & Guia de Configuração de Hosts

## 1. Visão Geral da Arquitetura

O objetivo desta especificação é estender o **Kraken Admin Tool** em duas frentes complementares:
1. **Autenticação SSH Completa (Chave/Certificado e Usuário/Senha)**: Permitir que o backend Go conecte-se a daemons Docker remotos via protocolo `ssh://` utilizando tanto **Chave Privada / Certificado SSH** quanto **Usuário e Senha**, através de um dialer SSH nativo (`golang.org/x/crypto/ssh`) com tunelamento direto para o socket Unix remoto (`/var/run/docker.sock`) ou fallback via `docker system dial-stdio`.
2. **Links e Diálogo Explicativo de Configuração (Host Setup Guide Modal)**: Em cada opção/item de adição de host (Local Socket, Remote TCP, Secure TLS, SSH com Chave/Certificado e SSH com Usuário/Senha) e nas seções do formulário, disponibilizar links interativos que abrem um diálogo detalhado explicando:
   - Como configurar o host remoto (comandos Linux, `daemon.json`, `systemd`, `sshd_config`, grupo `docker`, geração de certificados/chaves).
   - Como preencher os dados no diálogo de adição de host do Kraken Admin Tool.

---

## 2. Divisão em Tarefas Atômicas e Independentes

### Tarefa 1: Dialer SSH Nativo e Testes Unitários no Backend (`backend/internal/docker/ssh_dialer.go` & `backend/internal/docker/ssh_dialer_test.go`)
- **Objetivo**: Criar um módulo isolado no pacote `docker` responsável por parsear URLs `ssh://`, configurar o cliente `golang.org/x/crypto/ssh` (suportando autenticação por senha, keyboard-interactive, chave privada PEM/OpenSSH com ou sem passphrase, e chaves padrão do sistema) e fornecer uma função `DialContext` compatível com `http.Transport` e `client.WithDialContext`.
- **Assinaturas Principais**:
  ```go
  package docker

  import (
      "context"
      "net"
  )

  type SSHConfig struct {
      HostURL       string
      User          string
      AuthMethod    string // "password" | "key" | ""
      Password      string
      PrivateKey    string
      KeyPassphrase string
  }

  type ParsedSSHHost struct {
      User       string
      Host       string
      Port       string
      SocketPath string
  }

  func IsSSHHost(hostURL string) bool
  func ParseSSHHost(hostURL string, overrideUser string) (*ParsedSSHHost, error)
  func BuildSSHDialContext(cfg SSHConfig) (func(ctx context.Context, network, addr string) (net.Conn, error), error)
  ```
- **Detalhes de Implementação**:
  - `ParseSSHHost`: Extrai usuário, host, porta (default `22`) e caminho do socket (default `/var/run/docker.sock` se vazio) de URLs como `ssh://user@192.168.1.100:22` ou `ssh://192.168.1.100/run/user/1000/docker.sock`. Se `overrideUser` for fornecido, tem precedência sobre o usuário da URL.
  - `BuildSSHDialContext`: Constrói métodos `ssh.AuthMethod`:
    - Se `Password != ""` (ou `AuthMethod == "password"`): adiciona `ssh.Password(cfg.Password)` e `ssh.KeyboardInteractive` que responde com a senha.
    - Se `PrivateKey != ""` (ou `AuthMethod == "key"`): faz parse da chave privada via `ssh.ParsePrivateKey` ou `ssh.ParsePrivateKeyWithPassphrase`.
    - No `DialContext`: conecta via `ssh.Dial("tcp", net.JoinHostPort(parsed.Host, parsed.Port), clientConfig)`. Em seguida tenta `sshClient.Dial("unix", parsed.SocketPath)`. Se falhar, abre uma `ssh.Session` executando `docker system dial-stdio` encapsulada em um `net.Conn` customizado que fecha a sessão e o cliente SSH ao encerrar.
- **Validação**: Testes unitários em `backend/internal/docker/ssh_dialer_test.go` testando `IsSSHHost`, `ParseSSHHost` (com portas customizadas, override de usuário, socket customizado) e `BuildSSHDialContext` (validação de credenciais e handshake contra um servidor SSH local de teste em memória usando `golang.org/x/crypto/ssh`).

---

### Tarefa 2: Atualização dos Modelos e Serviço Docker no Backend (`backend/internal/models/models.go` & `backend/internal/docker/service.go`)
- **Objetivo**: Adicionar os campos de configuração SSH ao modelo `DockerServer` e aos DTOs (`CreateServerRequest`, `UpdateServerRequest`, `TestServerRequest`), e integrar `BuildSSHDialContext` em `createDockerClient` dentro de `service.go`.
- **Alterações em `backend/internal/models/models.go`**:
  - Em `DockerServer`:
    ```go
    SSHUser          string `gorm:"size:128" json:"ssh_user,omitempty"`
    SSHAuthMethod    string `gorm:"size:32" json:"ssh_auth_method,omitempty"` // "key" or "password"
    SSHPassword      string `gorm:"type:text" json:"-"`
    SSHPrivateKey    string `gorm:"type:text" json:"-"`
    SSHKeyPassphrase string `gorm:"type:text" json:"-"`
    ```
  - Em `CreateServerRequest`, `UpdateServerRequest`, `TestServerRequest`:
    ```go
    SSHUser          string `json:"ssh_user"`
    SSHAuthMethod    string `json:"ssh_auth_method"`
    SSHPassword      string `json:"ssh_password"`
    SSHPrivateKey    string `json:"ssh_private_key"`
    SSHKeyPassphrase string `json:"ssh_key_passphrase"`
    ```
- **Alterações em `backend/internal/docker/service.go`**:
  - Atualizar a criação do cliente Docker para aceitar as opções de SSH (via helper `createDockerClientWithConfig` mantendo compatibilidade com `createDockerClient`).
  - Se `IsSSHHost(cleanHost)` for verdadeiro:
    - Construir o `dialContext` com `BuildSSHDialContext`.
    - Configurar `httpClient := &http.Client{Transport: &http.Transport{DialContext: dialContext}, Timeout: 30 * time.Second}`.
    - Usar `client.WithHTTPClient(httpClient)`, `client.WithHost("http://docker")` e `client.WithDialContext(dialContext)`.
  - Em `CreateServer`, `UpdateServer`, `TestServer` e `GetClient`: propagar `SSHUser`, `SSHAuthMethod`, `SSHPassword`, `SSHPrivateKey`, `SSHKeyPassphrase`.
- **Validação**: `go test ./...` no diretório `backend`.

---

### Tarefa 3: Tipos Frontend e Componente de Diálogo Explicativo (`frontend/src/types/index.ts` & `frontend/src/components/modals/HostSetupGuideModal.tsx`)
- **Objetivo**: Atualizar as interfaces TypeScript e criar o modal interativo `HostSetupGuideModal` contendo a documentação passo a passo para cada método de adição de host e como configurar o servidor remoto.
- **Alterações em `frontend/src/types/index.ts`**:
  - Atualizar `DockerServer`, `CreateServerPayload`, `TestServerPayload` com:
    ```ts
    ssh_user?: string;
    ssh_auth_method?: "key" | "password" | string;
    ssh_password?: string;
    ssh_private_key?: string;
    ssh_key_passphrase?: string;
    ```
- **Novo Arquivo `frontend/src/components/modals/HostSetupGuideModal.tsx`**:
  - Exportar `export type HostGuideTopic = "local-socket" | "remote-tcp" | "secure-tls" | "ssh-key" | "ssh-password";`
  - Exportar `HostSetupGuideModal` recebendo:
    ```ts
    export interface HostSetupGuideModalProps {
      isOpen: boolean;
      onClose: () => void;
      initialTopic?: HostGuideTopic;
      onSelectPreset?: (topic: HostGuideTopic) => void;
    }
    ```
  - Conteúdo explicativo em Português para cada um dos 5 tópicos (`Local Socket`, `TCP Remoto (2375)`, `TLS Seguro (2376)`, `SSH por Chave/Certificado`, `SSH por Usuário e Senha`):
    1. **Resumo e Quando Utilizar** (nível de segurança e caso de uso).
    2. **Passo 1: Como Configurar o Host Remoto (Servidor Linux / Docker)** com blocos de código copiáveis (ex: permissões do `/var/run/docker.sock`, edição do `systemctl edit docker.service` ou `/etc/docker/daemon.json`, geração de certificados CA/Server/Client com `openssl`, configuração do `/etc/ssh/sshd_config` para `PubkeyAuthentication yes` ou `PasswordAuthentication yes`, adição do usuário ao grupo `docker` com `sudo usermod -aG docker $USER`, etc.).
    3. **Passo 2: Como Adicionar o Host no Kraken Admin Tool** explicando exatamente o que preencher em cada campo do formulário.
    4. Botão `"Usar este modelo no formulário"` (quando `onSelectPreset` for passado) que aplica automaticamente a configuração correspondente e fecha o guia.

---

### Tarefa 4: Atualização da Página de Servidores (`frontend/src/app/(dashboard)/servers/page.tsx`)
- **Objetivo**: Integrar a seleção de autenticação SSH (Chave/Certificado vs Usuário e Senha) e adicionar os links de ajuda contextual em cada item de adição de host e seção do modal para abrir o `HostSetupGuideModal`.
- **Alterações em `frontend/src/app/(dashboard)/servers/page.tsx`**:
  - Adicionar estados do formulário para SSH:
    - `sshUser`, `sshAuthMethod` (`"key" | "password"`), `sshPassword`, `sshPrivateKey`, `sshKeyPassphrase`.
  - Adicionar estados do modal de guia:
    - `guideModalOpen` (`boolean`) e `guideTopic` (`HostGuideTopic`).
  - Nos **Templates de Conexão (itens de adição de host)**:
    - Exibir cards/itens para:
      1. `Local Socket` (`unix:///var/run/docker.sock`) + link `"Como configurar?"`
      2. `Remote TCP (Port 2375)` (`tcp://192.168.1.100:2375`) + link `"Como configurar?"`
      3. `Secure TLS (Port 2376)` (`tcp://192.168.1.100:2376`) + link `"Como configurar?"`
      4. `SSH (Chave / Certificado)` (`ssh://root@192.168.1.100:22`, `sshAuthMethod = "key"`) + link `"Como configurar?"`
      5. `SSH (Usuário e Senha)` (`ssh://root@192.168.1.100:22`, `sshAuthMethod = "password"`) + link `"Como configurar?"`
  - Na seção de **Configuração SSH** (exibida automaticamente quando `hostUrl.startsWith("ssh://")` ou ao selecionar um template SSH):
    - Link no cabeçalho da seção: `"Ver guia de configuração SSH no host remoto"` (abre o `HostSetupGuideModal` no tópico `ssh-key` ou `ssh-password`).
    - Seletor do método de autenticação: **Chave SSH / Certificado** (`key`) ou **Usuário e Senha** (`password`).
    - Campo **Usuário SSH** (ex: `root`, `ubuntu`, `docker-admin`) que sincroniza ou complementa o `user@` na URL.
    - Se `sshAuthMethod === "password"`: campo de **Senha SSH** (`type="password"`).
    - Se `sshAuthMethod === "key"`: campo `textarea` para **Chave Privada / Certificado SSH (PEM / OpenSSH)** e campo opcional **Passphrase da Chave**.
  - Na seção de **TLS / SSL Encryption**:
    - Adicionar link `"Como gerar certificados e configurar TLS?"` que abre o `HostSetupGuideModal` no tópico `secure-tls`.
  - Na tabela de servidores (coluna `Security`):
    - Identificar servidores `ssh://` exibindo badge indicando `SSH (Password)` ou `SSH (Key/Cert)`.
  - Enviar campos `ssh_user`, `ssh_auth_method`, `ssh_password`, `ssh_private_key`, `ssh_key_passphrase` tanto em `handleTestConnection` quanto em `handleSaveServer`.
- **Validação**: `npm run build` (ou `npx tsc --noEmit`) no diretório `frontend`.
