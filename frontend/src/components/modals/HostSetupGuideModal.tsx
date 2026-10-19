"use client";

import React, { useState, useEffect } from "react";
import { Modal } from "@/components/ui/Modal";
import { Button } from "@/components/ui/Button";
import {
  Terminal,
  Shield,
  Key,
  Lock,
  Server,
  Copy,
  Check,
  BookOpen,
  Info,
  CheckCircle2,
  AlertTriangle,
  ArrowRight,
} from "lucide-react";

export type HostGuideTopic =
  | "local-socket"
  | "remote-tcp"
  | "secure-tls"
  | "ssh-key"
  | "ssh-password";

export interface HostSetupGuideModalProps {
  isOpen: boolean;
  onClose: () => void;
  initialTopic?: HostGuideTopic;
  onSelectPreset?: (topic: HostGuideTopic) => void;
}

interface CodeBlockProps {
  code: string;
  title?: string;
}

function CodeBlock({ code, title }: CodeBlockProps) {
  const [copied, setCopied] = useState(false);

  const handleCopy = async () => {
    try {
      await navigator.clipboard.writeText(code.trim());
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    } catch (err) {
      console.error("Failed to copy command:", err);
    }
  };

  return (
    <div className="rounded-lg border border-slate-800 bg-slate-950/90 overflow-hidden my-2 shadow-sm">
      <div className="flex items-center justify-between border-b border-slate-800/80 bg-slate-900/60 px-3 py-1.5 text-xs text-slate-400">
        <span className="flex items-center gap-1.5 font-mono text-[11px] text-slate-300">
          <Terminal className="h-3.5 w-3.5 text-sky-400" />
          {title || "Comando Bash"}
        </span>
        <button
          type="button"
          onClick={handleCopy}
          className="flex items-center gap-1 rounded px-1.5 py-0.5 text-[11px] text-slate-400 hover:bg-slate-800 hover:text-white transition-colors"
          title="Copiar comando"
        >
          {copied ? (
            <>
              <Check className="h-3 w-3 text-emerald-400" />
              <span className="text-emerald-400 font-medium">Copiado!</span>
            </>
          ) : (
            <>
              <Copy className="h-3 w-3" />
              <span>Copiar</span>
            </>
          )}
        </button>
      </div>
      <div className="p-3 font-mono text-xs text-slate-200 overflow-x-auto whitespace-pre leading-relaxed selection:bg-sky-500/30">
        <code>{code.trim()}</code>
      </div>
    </div>
  );
}

export function HostSetupGuideModal({
  isOpen,
  onClose,
  initialTopic = "ssh-password",
  onSelectPreset,
}: HostSetupGuideModalProps) {
  const [activeTopic, setActiveTopic] = useState<HostGuideTopic>(initialTopic);

  useEffect(() => {
    if (isOpen && initialTopic) {
      setActiveTopic(initialTopic);
    }
  }, [isOpen, initialTopic]);

  const tabs: {
    id: HostGuideTopic;
    label: string;
    icon: React.ComponentType<{ className?: string }>;
    badge: string;
  }[] = [
    {
      id: "local-socket",
      label: "Local Socket",
      icon: Terminal,
      badge: "Local",
    },
    {
      id: "remote-tcp",
      label: "Remote TCP",
      icon: Server,
      badge: "Porta 2375",
    },
    {
      id: "secure-tls",
      label: "Secure TLS",
      icon: Shield,
      badge: "mTLS 2376",
    },
    {
      id: "ssh-key",
      label: "SSH por Chave",
      icon: Key,
      badge: "Recomendado",
    },
    {
      id: "ssh-password",
      label: "SSH por Senha",
      icon: Lock,
      badge: "Porta 22",
    },
  ];

  return (
    <Modal
      isOpen={isOpen}
      onClose={onClose}
      title="Guia de Configuração de Hosts Docker"
      description="Instruções passo a passo para preparar o host remoto e conectar ao Kraken Admin Tool."
      maxWidth="2xl"
    >
      <div className="space-y-4">
        {/* Navigation Tabs */}
        <div className="flex items-center gap-1 overflow-x-auto pb-1 border-b border-slate-800 scrollbar-thin">
          {tabs.map((tab) => {
            const Icon = tab.icon;
            const isActive = activeTopic === tab.id;
            return (
              <button
                key={tab.id}
                type="button"
                onClick={() => setActiveTopic(tab.id)}
                className={`flex items-center gap-1.5 whitespace-nowrap rounded-lg px-3 py-2 text-xs font-medium transition-all ${
                  isActive
                    ? "bg-sky-500/10 text-sky-400 border border-sky-500/30 shadow-sm"
                    : "text-slate-400 hover:bg-slate-800/60 hover:text-slate-200 border border-transparent"
                }`}
              >
                <Icon className={`h-3.5 w-3.5 ${isActive ? "text-sky-400" : "text-slate-500"}`} />
                <span>{tab.label}</span>
                <span
                  className={`ml-1 rounded px-1.5 py-0.2 text-[10px] ${
                    isActive
                      ? "bg-sky-400/20 text-sky-300"
                      : "bg-slate-800 text-slate-400"
                  }`}
                >
                  {tab.badge}
                </span>
              </button>
            );
          })}
        </div>

        {/* Content Area with custom scroll */}
        <div className="max-h-[70vh] overflow-y-auto pr-1 space-y-4 text-xs text-slate-300">
          {/* TAB 1: LOCAL SOCKET */}
          {activeTopic === "local-socket" && (
            <div className="space-y-4">
              {/* Overview & Security */}
              <div className="rounded-xl border border-slate-800 bg-slate-950/60 p-3.5 space-y-2">
                <div className="flex items-center gap-2 text-slate-100 font-semibold text-sm">
                  <Terminal className="h-4 w-4 text-sky-400" />
                  <span>Visão Geral & Recomendação de Segurança</span>
                </div>
                <p className="text-slate-300 leading-relaxed">
                  A conexão via <strong>Unix Domain Socket</strong> (<code>unix:///var/run/docker.sock</code>) é a forma nativa e mais rápida para comunicar com o daemon Docker quando o Kraken Admin Tool é executado na mesma máquina host ou montado em um container local.
                </p>
                <div className="flex items-start gap-2 rounded-lg border border-emerald-500/20 bg-emerald-500/10 p-2.5 text-emerald-300 text-[11px] leading-relaxed">
                  <CheckCircle2 className="h-4 w-4 shrink-0 text-emerald-400 mt-0.5" />
                  <span>
                    <strong>Seguro por Padrão:</strong> A comunicação não transita por interfaces de rede IP externas. O acesso é restrito localmente pelas permissões de arquivo do sistema de arquivos POSIX.
                  </span>
                </div>
              </div>

              {/* Step 1: Remote Host Config */}
              <div className="rounded-xl border border-slate-800 bg-slate-950/60 p-3.5 space-y-3">
                <div className="flex items-center gap-2 text-slate-100 font-semibold text-sm">
                  <BookOpen className="h-4 w-4 text-indigo-400" />
                  <span>Passo 1: Como Configurar o Host (Linux / Docker)</span>
                </div>

                <div className="space-y-2">
                  <p className="text-slate-300 font-medium">1. Verifique se o daemon Docker está em execução e habilitado:</p>
                  <CodeBlock
                    title="Habilitar e iniciar Docker daemon"
                    code="sudo systemctl enable --now docker"
                  />
                </div>

                <div className="space-y-2">
                  <p className="text-slate-300 font-medium">2. Ajuste as permissões do grupo Docker para que seu usuário acesse o socket sem precisar de sudo:</p>
                  <CodeBlock
                    title="Adicionar usuário ao grupo docker e definir permissões"
                    code="sudo usermod -aG docker $USER && sudo chmod 660 /var/run/docker.sock"
                  />
                  <p className="text-[11px] text-slate-400 italic">
                    * Após executar o comando acima, encerre a sessão do terminal e conecte-se novamente para que o novo grupo tenha efeito.
                  </p>
                </div>

                <div className="space-y-2">
                  <p className="text-slate-300 font-medium">3. Se o Kraken estiver rodando dentro de um container Docker, mapeie o volume do socket:</p>
                  <CodeBlock
                    title="Montagem de volume do socket no container"
                    code="docker run -d -p 8080:8080 -v /var/run/docker.sock:/var/run/docker.sock --name kraken-admin kraken-admin-tool"
                  />
                </div>
              </div>

              {/* Step 2: Form Instructions */}
              <div className="rounded-xl border border-slate-800 bg-slate-950/60 p-3.5 space-y-3">
                <div className="flex items-center gap-2 text-slate-100 font-semibold text-sm">
                  <Info className="h-4 w-4 text-sky-400" />
                  <span>Passo 2: Como Adicionar no Kraken Admin Tool</span>
                </div>
                <ol className="list-decimal list-inside space-y-2 text-slate-300 leading-relaxed">
                  <li>No campo <strong>Friendly Name</strong>, digite um nome identificador (ex: <code className="text-sky-300">Local Docker Engine</code>).</li>
                  <li>No campo <strong>Docker Host URL</strong>, insira exatamente: <code className="text-sky-300">unix:///var/run/docker.sock</code>.</li>
                  <li>Deixe a opção <strong>TLS / SSL Encryption</strong> desmarcada (não aplicável para sockets Unix).</li>
                  <li>Clique no botão <strong>Test Connection</strong>. Você deve ver a mensagem de sucesso indicando a versão do Docker conectado.</li>
                  <li>Clique em <strong>Save Server</strong> para concluir.</li>
                </ol>
              </div>
            </div>
          )}

          {/* TAB 2: REMOTE TCP */}
          {activeTopic === "remote-tcp" && (
            <div className="space-y-4">
              {/* Overview & Security */}
              <div className="rounded-xl border border-slate-800 bg-slate-950/60 p-3.5 space-y-2">
                <div className="flex items-center gap-2 text-slate-100 font-semibold text-sm">
                  <Server className="h-4 w-4 text-amber-400" />
                  <span>Visão Geral & Recomendação de Segurança</span>
                </div>
                <p className="text-slate-300 leading-relaxed">
                  A porta TCP <strong>2375</strong> expõe a API REST do Docker pela rede sem autenticação ou criptografia.
                </p>
                <div className="flex items-start gap-2 rounded-lg border border-amber-500/30 bg-amber-500/10 p-3 text-amber-300 text-[11px] leading-relaxed">
                  <AlertTriangle className="h-4 w-4 shrink-0 text-amber-400 mt-0.5" />
                  <span>
                    <strong>ALERTA CRÍTICO DE SEGURANÇA:</strong> Qualquer máquina com acesso à porta 2375 tem privilégios totais de root sobre o servidor remoto. Use esta opção <strong>EXCLUSIVAMENTE</strong> em redes locais 100% isoladas, redes de laboratório internas ou através de um túnel VPN seguro (WireGuard, Tailscale). <strong>NUNCA exponha a porta 2375 na Internet pública!</strong>
                  </span>
                </div>
              </div>

              {/* Step 1: Remote Host Config */}
              <div className="rounded-xl border border-slate-800 bg-slate-950/60 p-3.5 space-y-3">
                <div className="flex items-center gap-2 text-slate-100 font-semibold text-sm">
                  <BookOpen className="h-4 w-4 text-indigo-400" />
                  <span>Passo 1: Como Configurar o Host Remoto (Linux / Docker)</span>
                </div>

                <div className="space-y-2">
                  <p className="text-slate-300 font-medium">1. Crie um override para o serviço Docker no systemd:</p>
                  <CodeBlock
                    title="Abrir editor de override do systemd"
                    code="sudo systemctl edit docker.service"
                  />
                  <p className="text-slate-400 text-[11px]">Cole o bloco de configuração abaixo no editor que abrir:</p>
                  <CodeBlock
                    title="Configuração do systemd override (/etc/systemd/system/docker.service.d/override.conf)"
                    code={`[Service]\nExecStart=\nExecStart=/usr/bin/dockerd -H fd:// -H tcp://0.0.0.0:2375`}
                  />
                  <p className="text-[11px] text-slate-400 italic">
                    * Alternativamente, se seu Docker não utilizar flags no systemd, adicione <code>{`{"hosts": ["unix:///var/run/docker.sock", "tcp://0.0.0.0:2375"]}`}</code> ao <code>/etc/docker/daemon.json</code>.
                  </p>
                </div>

                <div className="space-y-2">
                  <p className="text-slate-300 font-medium">2. Recarregue os serviços do systemd e reinicie o Docker:</p>
                  <CodeBlock
                    title="Recarregar systemd e reiniciar daemon"
                    code="sudo systemctl daemon-reload && sudo systemctl restart docker"
                  />
                </div>

                <div className="space-y-2">
                  <p className="text-slate-300 font-medium">3. Configure o Firewall (UFW) liberando apenas para a rede segura ou IP do Kraken:</p>
                  <CodeBlock
                    title="Liberar porta 2375 apenas para o IP de origem confiável"
                    code="sudo ufw allow from IP_DO_KRAKEN to any port 2375 proto tcp"
                  />
                </div>
              </div>

              {/* Step 2: Form Instructions */}
              <div className="rounded-xl border border-slate-800 bg-slate-950/60 p-3.5 space-y-3">
                <div className="flex items-center gap-2 text-slate-100 font-semibold text-sm">
                  <Info className="h-4 w-4 text-sky-400" />
                  <span>Passo 2: Como Adicionar no Kraken Admin Tool</span>
                </div>
                <ol className="list-decimal list-inside space-y-2 text-slate-300 leading-relaxed">
                  <li>No campo <strong>Friendly Name</strong>, digite um nome (ex: <code className="text-sky-300">Lab Worker Node 01</code>).</li>
                  <li>No campo <strong>Docker Host URL</strong>, informe a URL com a porta 2375: <code className="text-sky-300">tcp://IP_DO_SERVIDOR:2375</code>.</li>
                  <li>Mantenha a opção <strong>TLS / SSL Encryption</strong> desmarcada.</li>
                  <li>Clique em <strong>Test Connection</strong> para verificar se o host remoto responde à requisição.</li>
                  <li>Clique em <strong>Save Server</strong>.</li>
                </ol>
              </div>
            </div>
          )}

          {/* TAB 3: SECURE TLS */}
          {activeTopic === "secure-tls" && (
            <div className="space-y-4">
              {/* Overview & Security */}
              <div className="rounded-xl border border-slate-800 bg-slate-950/60 p-3.5 space-y-2">
                <div className="flex items-center gap-2 text-slate-100 font-semibold text-sm">
                  <Shield className="h-4 w-4 text-sky-400" />
                  <span>Visão Geral & Recomendação de Segurança</span>
                </div>
                <p className="text-slate-300 leading-relaxed">
                  A porta TCP <strong>2376</strong> combinada com <strong>Mutual TLS (mTLS)</strong> é a recomendação oficial do Docker para expor o daemon pela rede de forma criptografada e autenticada por certificados X.509.
                </p>
                <div className="flex items-start gap-2 rounded-lg border border-sky-500/20 bg-sky-500/10 p-2.5 text-sky-300 text-[11px] leading-relaxed">
                  <CheckCircle2 className="h-4 w-4 shrink-0 text-sky-400 mt-0.5" />
                  <span>
                    <strong>Autenticação Mútua Forte:</strong> Tanto o servidor remoto quanto o cliente Kraken validam a Autoridade Certificadora (CA). Conexões sem certificados assinados pela CA são imediatamente recusadas no nível TLS.
                  </span>
                </div>
              </div>

              {/* Step 1: Remote Host Config */}
              <div className="rounded-xl border border-slate-800 bg-slate-950/60 p-3.5 space-y-3">
                <div className="flex items-center gap-2 text-slate-100 font-semibold text-sm">
                  <BookOpen className="h-4 w-4 text-indigo-400" />
                  <span>Passo 1: Como Configurar o Host Remoto (Geração OpenSSL & Daemon)</span>
                </div>

                <div className="space-y-2">
                  <p className="text-slate-300 font-medium">1. Crie a Autoridade Certificadora (CA):</p>
                  <CodeBlock
                    title="Gerar Chave e Certificado Raiz CA"
                    code={`openssl genrsa -out ca-key.pem 4096\nopenssl req -new -x509 -days 3650 -key ca-key.pem -sha256 -out ca.pem -subj "/CN=Docker-CA"`}
                  />
                </div>

                <div className="space-y-2">
                  <p className="text-slate-300 font-medium">2. Gere a Chave e Certificado do Servidor Docker (substitua <code>SEU_HOST_OU_IP</code>):</p>
                  <CodeBlock
                    title="Gerar Certificado do Servidor com SAN"
                    code={`openssl genrsa -out server-key.pem 4096\nopenssl req -subj "/CN=SEU_HOST_OU_IP" -sha256 -new -key server-key.pem -out server.csr\necho "subjectAltName = DNS:SEU_HOST_OU_IP,IP:SEU_HOST_OU_IP,IP:127.0.0.1" > extfile.cnf\necho "extendedKeyUsage = serverAuth" >> extfile.cnf\nopenssl x509 -req -days 365 -sha256 -in server.csr -CA ca.pem -CAkey ca-key.pem -CAcreateserial -out server-cert.pem -extfile extfile.cnf`}
                  />
                </div>

                <div className="space-y-2">
                  <p className="text-slate-300 font-medium">3. Gere a Chave e Certificado do Cliente (para usar no Kraken Admin Tool):</p>
                  <CodeBlock
                    title="Gerar Certificado do Cliente Kraken"
                    code={`openssl genrsa -out key.pem 4096\nopenssl req -subj "/CN=client" -new -key key.pem -out client.csr\necho "extendedKeyUsage = clientAuth" > extfile-client.cnf\nopenssl x509 -req -days 365 -sha256 -in client.csr -CA ca.pem -CAkey ca-key.pem -CAcreateserial -out cert.pem -extfile extfile-client.cnf`}
                  />
                </div>

                <div className="space-y-2">
                  <p className="text-slate-300 font-medium">4. Instale os certificados no host e configure o daemon Docker:</p>
                  <CodeBlock
                    title="Copiar certificados e proteger chave privada"
                    code={`sudo mkdir -p /etc/docker/certs\nsudo cp ca.pem server-cert.pem server-key.pem /etc/docker/certs/\nsudo chmod 0400 /etc/docker/certs/server-key.pem`}
                  />
                  <p className="text-slate-400 text-[11px]">Adicione no arquivo <code>/etc/docker/daemon.json</code>:</p>
                  <CodeBlock
                    title="Configuração TLS em /etc/docker/daemon.json"
                    code={`{\n  "tlsverify": true,\n  "tlscacert": "/etc/docker/certs/ca.pem",\n  "tlscert": "/etc/docker/certs/server-cert.pem",\n  "tlskey": "/etc/docker/certs/server-key.pem",\n  "hosts": ["unix:///var/run/docker.sock", "tcp://0.0.0.0:2376"]\n}`}
                  />
                  <p className="text-slate-300 font-medium">Em seguida, reinicie o daemon Docker:</p>
                  <CodeBlock
                    title="Reiniciar serviço Docker com mTLS"
                    code="sudo systemctl restart docker"
                  />
                </div>
              </div>

              {/* Step 2: Form Instructions */}
              <div className="rounded-xl border border-slate-800 bg-slate-950/60 p-3.5 space-y-3">
                <div className="flex items-center gap-2 text-slate-100 font-semibold text-sm">
                  <Info className="h-4 w-4 text-sky-400" />
                  <span>Passo 2: Como Adicionar no Kraken Admin Tool</span>
                </div>
                <ol className="list-decimal list-inside space-y-2 text-slate-300 leading-relaxed">
                  <li>No campo <strong>Friendly Name</strong>, digite um nome (ex: <code className="text-sky-300">Production Node (TLS)</code>).</li>
                  <li>No campo <strong>Docker Host URL</strong>, informe: <code className="text-sky-300">tcp://SEU_HOST_OU_IP:2376</code>.</li>
                  <li>Ative a opção <strong>TLS / SSL Encryption (Port 2376)</strong>.</li>
                  <li>Cole o conteúdo do arquivo <code className="text-indigo-300">ca.pem</code> no campo <strong>CA Certificate</strong>.</li>
                  <li>Cole o conteúdo do arquivo <code className="text-indigo-300">cert.pem</code> no campo <strong>Client Certificate</strong>.</li>
                  <li>Cole o conteúdo do arquivo <code className="text-indigo-300">key.pem</code> no campo <strong>Client Private Key</strong>.</li>
                  <li>Clique em <strong>Test Connection</strong> para verificar o handshake TLS e salve o host.</li>
                </ol>
              </div>
            </div>
          )}

          {/* TAB 4: SSH KEY */}
          {activeTopic === "ssh-key" && (
            <div className="space-y-4">
              {/* Overview & Security */}
              <div className="rounded-xl border border-slate-800 bg-slate-950/60 p-3.5 space-y-2">
                <div className="flex items-center gap-2 text-slate-100 font-semibold text-sm">
                  <Key className="h-4 w-4 text-emerald-400" />
                  <span>Visão Geral & Recomendação de Segurança</span>
                </div>
                <p className="text-slate-300 leading-relaxed">
                  A conexão <strong>SSH por Chave Privada / Certificado</strong> é a opção mais recomendada e moderna para gerenciar servidores remotos em nuvem (AWS, GCP, DigitalOcean, Hetzner, etc.).
                </p>
                <div className="flex items-start gap-2 rounded-lg border border-emerald-500/20 bg-emerald-500/10 p-2.5 text-emerald-300 text-[11px] leading-relaxed">
                  <CheckCircle2 className="h-4 w-4 shrink-0 text-emerald-400 mt-0.5" />
                  <span>
                    <strong>Dispensa Abertura de Novas Portas:</strong> Utiliza o canal SSH seguro existente (porta padrão 22) e tunela diretamente para o socket <code>/var/run/docker.sock</code> do host remoto. Criptografia ponta a ponta sem riscos de força bruta de senhas.
                  </span>
                </div>
              </div>

              {/* Step 1: Remote Host Config */}
              <div className="rounded-xl border border-slate-800 bg-slate-950/60 p-3.5 space-y-3">
                <div className="flex items-center gap-2 text-slate-100 font-semibold text-sm">
                  <BookOpen className="h-4 w-4 text-indigo-400" />
                  <span>Passo 1: Como Configurar o Host Remoto (Linux / SSH)</span>
                </div>

                <div className="space-y-2">
                  <p className="text-slate-300 font-medium">1. Adicione seu usuário ao grupo <code>docker</code> no servidor remoto:</p>
                  <CodeBlock
                    title="Permitir acesso ao socket Docker sem sudo interativo"
                    code="sudo usermod -aG docker SEU_USUARIO"
                  />
                  <p className="text-[11px] text-slate-400 italic">
                    * Isso permite que o dialer SSH do Kraken acesse <code>/var/run/docker.sock</code> sem ser bloqueado por permissão de arquivo.
                  </p>
                </div>

                <div className="space-y-2">
                  <p className="text-slate-300 font-medium">2. Gere um par de chaves SSH (ED25519 recomendado):</p>
                  <CodeBlock
                    title="Gerar par de chaves SSH ed25519"
                    code='ssh-keygen -t ed25519 -C "kraken-admin"'
                  />
                </div>

                <div className="space-y-2">
                  <p className="text-slate-300 font-medium">3. Instale a chave pública no servidor remoto:</p>
                  <CodeBlock
                    title="Copiar chave pública para o host remoto"
                    code="ssh-copy-id -i ~/.ssh/id_ed25519.pub SEU_USUARIO@IP_DO_HOST"
                  />
                  <p className="text-[11px] text-slate-400 italic">
                    * Ou adicione o conteúdo de <code>~/.ssh/id_ed25519.pub</code> manualmente no arquivo <code>~/.ssh/authorized_keys</code> do servidor remoto.
                  </p>
                </div>

                <div className="space-y-2">
                  <p className="text-slate-300 font-medium">4. Certifique-se de que a autenticação por chave pública está ativa no SSH:</p>
                  <CodeBlock
                    title="Verificar PubkeyAuthentication no /etc/ssh/sshd_config"
                    code='sudo grep -E "PubkeyAuthentication" /etc/ssh/sshd_config'
                  />
                  <p className="text-[11px] text-slate-400">
                    Deve conter <code>PubkeyAuthentication yes</code>. Se precisar alterar, reinicie o SSH com <code>sudo systemctl restart sshd</code>.
                  </p>
                </div>

                <div className="space-y-2">
                  <p className="text-slate-300 font-medium">5. Teste o comando Docker via SSH para confirmar:</p>
                  <CodeBlock
                    title="Testar execução via SSH"
                    code='ssh -i ~/.ssh/id_ed25519 SEU_USUARIO@IP_DO_HOST "docker info"'
                  />
                </div>

                <div className="rounded-lg border border-indigo-500/20 bg-indigo-500/10 p-3 text-[11px] text-indigo-300 space-y-1">
                  <p className="font-semibold flex items-center gap-1.5">
                    <Server className="h-3.5 w-3.5 text-indigo-400" />
                    Gerenciamento Universal de Kubernetes (kubectl)
                  </p>
                  <p className="text-slate-300">
                    Se este host também roda Kubernetes, certifique-se de que o comando universal <code className="text-sky-300">kubectl</code> esteja instalado no servidor e com acesso ao cluster (<code className="text-sky-300">kubectl get nodes</code>). O Kraken se apoia exclusivamente em comandos padronizados <code className="text-sky-300">kubectl</code> via SSH.
                  </p>
                </div>
              </div>

              {/* Step 2: Form Instructions */}
              <div className="rounded-xl border border-slate-800 bg-slate-950/60 p-3.5 space-y-3">
                <div className="flex items-center gap-2 text-slate-100 font-semibold text-sm">
                  <Info className="h-4 w-4 text-sky-400" />
                  <span>Passo 2: Como Adicionar no Kraken Admin Tool</span>
                </div>
                <ol className="list-decimal list-inside space-y-2 text-slate-300 leading-relaxed">
                  <li>No campo <strong>Friendly Name</strong>, digite um nome (ex: <code className="text-sky-300">AWS Cloud Worker (SSH)</code>).</li>
                  <li>No campo <strong>Docker Host URL</strong>, informe a URL: <code className="text-sky-300">ssh://SEU_USUARIO@IP_DO_HOST:22</code>.</li>
                  <li>Na seção <strong>SSH Configuration</strong>, selecione o método <strong>Chave SSH / Certificado</strong>.</li>
                  <li>Confirme o <strong>SSH User</strong> (ex: <code className="text-indigo-300">ubuntu</code>, <code className="text-indigo-300">root</code> ou <code className="text-indigo-300">deployer</code>).</li>
                  <li>Cole o conteúdo da sua chave privada (geralmente em <code className="text-indigo-300">~/.ssh/id_ed25519</code> ou <code className="text-indigo-300">~/.ssh/id_rsa</code>) no campo <strong>SSH Private Key</strong>.</li>
                  <li>Se sua chave possuir senha, digite-a no campo <strong>Key Passphrase</strong> (opcional).</li>
                  <li>Clique em <strong>Test Connection</strong> para verificar o túnel SSH e salve o servidor.</li>
                </ol>
              </div>
            </div>
          )}

          {/* TAB 5: SSH PASSWORD */}
          {activeTopic === "ssh-password" && (
            <div className="space-y-4">
              {/* Overview & Security */}
              <div className="rounded-xl border border-slate-800 bg-slate-950/60 p-3.5 space-y-2">
                <div className="flex items-center gap-2 text-slate-100 font-semibold text-sm">
                  <Lock className="h-4 w-4 text-sky-400" />
                  <span>Visão Geral & Recomendação de Segurança</span>
                </div>
                <p className="text-slate-300 leading-relaxed">
                  A autenticação <strong>SSH por Usuário e Senha</strong> conecta ao servidor via túnel criptografado SSH na porta 22 utilizando as credenciais clássicas do usuário Linux.
                </p>
                <div className="flex items-start gap-2 rounded-lg border border-sky-500/20 bg-sky-500/10 p-2.5 text-sky-300 text-[11px] leading-relaxed">
                  <Info className="h-4 w-4 shrink-0 text-sky-400 mt-0.5" />
                  <span>
                    <strong>Simplicidade Imediata:</strong> Excelente para servidores internos ou quando você ainda não provisionou chaves públicas. A conexão é criptografada pelo SSH, mas recomenda-se o uso de senhas fortes para mitigar ataques de força bruta.
                  </span>
                </div>
              </div>

              {/* Step 1: Remote Host Config */}
              <div className="rounded-xl border border-slate-800 bg-slate-950/60 p-3.5 space-y-3">
                <div className="flex items-center gap-2 text-slate-100 font-semibold text-sm">
                  <BookOpen className="h-4 w-4 text-indigo-400" />
                  <span>Passo 1: Como Configurar o Host Remoto (Linux / SSH)</span>
                </div>

                <div className="space-y-2">
                  <p className="text-slate-300 font-medium">1. Adicione o usuário Linux remoto ao grupo <code>docker</code>:</p>
                  <CodeBlock
                    title="Adicionar usuário ao grupo docker para acesso ao socket"
                    code="sudo usermod -aG docker SEU_USUARIO"
                  />
                  <p className="text-[11px] text-slate-400 italic">
                    * Isso é fundamental para que o Docker aceite conexões do usuário sem necessidade de invocar <code>sudo</code> interativo.
                  </p>
                </div>

                <div className="space-y-2">
                  <p className="text-slate-300 font-medium">2. Certifique-se de que a autenticação por senha está habilitada no OpenSSH:</p>
                  <CodeBlock
                    title="Habilitar PasswordAuthentication no sshd_config"
                    code="sudo sed -i 's/^#*PasswordAuthentication.*/PasswordAuthentication yes/' /etc/ssh/sshd_config && sudo systemctl restart sshd"
                  />
                </div>

                <div className="space-y-2">
                  <p className="text-slate-300 font-medium">3. Teste o acesso e a execução do Docker a partir da sua máquina:</p>
                  <CodeBlock
                    title="Testar conexão SSH e comando docker info"
                    code='ssh SEU_USUARIO@IP_DO_HOST "docker info"'
                  />
                </div>

                <div className="rounded-lg border border-indigo-500/20 bg-indigo-500/10 p-3 text-[11px] text-indigo-300 space-y-1">
                  <p className="font-semibold flex items-center gap-1.5">
                    <Server className="h-3.5 w-3.5 text-indigo-400" />
                    Gerenciamento Universal de Kubernetes (kubectl)
                  </p>
                  <p className="text-slate-300">
                    Se este host também roda Kubernetes, certifique-se de que o comando universal <code className="text-sky-300">kubectl</code> esteja instalado no servidor e com acesso ao cluster (<code className="text-sky-300">kubectl get nodes</code>). O Kraken se apoia exclusivamente em comandos padronizados <code className="text-sky-300">kubectl</code> via SSH.
                  </p>
                </div>
              </div>

              {/* Step 2: Form Instructions */}
              <div className="rounded-xl border border-slate-800 bg-slate-950/60 p-3.5 space-y-3">
                <div className="flex items-center gap-2 text-slate-100 font-semibold text-sm">
                  <Info className="h-4 w-4 text-sky-400" />
                  <span>Passo 2: Como Adicionar no Kraken Admin Tool</span>
                </div>
                <ol className="list-decimal list-inside space-y-2 text-slate-300 leading-relaxed">
                  <li>No campo <strong>Friendly Name</strong>, digite um nome amigável (ex: <code className="text-sky-300">Staging VPS (SSH Password)</code>).</li>
                  <li>No campo <strong>Docker Host URL</strong>, informe a URL: <code className="text-sky-300">ssh://SEU_USUARIO@IP_DO_HOST:22</code>.</li>
                  <li>Na seção <strong>SSH Configuration</strong>, selecione a opção <strong>Usuário e Senha</strong>.</li>
                  <li>Preencha o campo <strong>SSH User</strong> com o nome do usuário remoto.</li>
                  <li>Preencha o campo <strong>SSH Password</strong> com a senha do usuário.</li>
                  <li>Clique em <strong>Test Connection</strong> para que o backend realize o handshake SSH e verifique o socket.</li>
                  <li>Clique em <strong>Save Server</strong> para concluir.</li>
                </ol>
              </div>
            </div>
          )}
        </div>

        {/* Modal Footer */}
        <div className="flex items-center justify-between pt-3 border-t border-slate-800">
          <div className="text-[11px] text-slate-500 flex items-center gap-1.5">
            <Info className="h-3.5 w-3.5" />
            <span>Selecione a aba desejada para ver o guia correspondente.</span>
          </div>

          <div className="flex items-center gap-2">
            <Button type="button" variant="outline" size="sm" onClick={onClose}>
              Fechar
            </Button>
            {onSelectPreset && (
              <Button
                type="button"
                variant="docker"
                size="sm"
                onClick={() => {
                  onSelectPreset(activeTopic);
                  onClose();
                }}
              >
                Aplicar este Modelo no Formulário
                <ArrowRight className="ml-1.5 h-3.5 w-3.5" />
              </Button>
            )}
          </div>
        </div>
      </div>
    </Modal>
  );
}
