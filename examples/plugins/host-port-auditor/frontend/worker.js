// Host Port Auditor - Web Worker Frontend
// Executado em sandbox Web Worker isolado (sem acesso a DOM, cookies ou localStorage)

let auditState = {
  lastScan: "Nunca",
  scannedCount: 4,
  riskCount: 2,
  ports: [
    { port: 22, proto: "TCP", service: "OpenSSH", host: "srv-prod-01", status: "ATENÇÃO", risk: "Médio" },
    { port: 80, proto: "TCP", service: "Nginx Ingress", host: "srv-prod-01", status: "OK", risk: "Baixo" },
    { port: 443, proto: "TCP", service: "Nginx TLS", host: "srv-prod-01", status: "OK", risk: "Baixo" },
    { port: 5432, proto: "TCP", service: "PostgreSQL", host: "db-node-02", status: "CRÍTICO", risk: "Alto" },
    { port: 6379, proto: "TCP", service: "Redis Cache", host: "cache-node-01", status: "CRÍTICO", risk: "Alto" },
    { port: 8080, proto: "TCP", service: "Kraken API", host: "localhost", status: "OK", risk: "Baixo" }
  ]
};

function renderDashboard() {
  kraken.render({
    type: "Page",
    title: "Auditor de Portas e Serviços de Rede",
    description: "Inspeção de conformidade de portas TCP expostas nos nós de infraestrutura e containers gerenciados",
    children: [
      {
        type: "Grid",
        children: [
          {
            type: "StatCard",
            title: "Total de Portas Monitoradas",
            value: String(auditState.ports.length),
            description: "Endpoints TCP ativos",
            variant: "running",
            label: "Monitorado"
          },
          {
            type: "StatCard",
            title: "Portas de Alto Risco",
            value: String(auditState.riskCount),
            description: "DBs e caches expostos",
            variant: "error",
            label: "Ação Necessária"
          },
          {
            type: "StatCard",
            title: "Servidores Avaliados",
            value: String(auditState.scannedCount),
            description: "Nós de produção e staging",
            variant: "docker",
            label: "Auditado"
          },
          {
            type: "StatCard",
            title: "Última Varredura",
            value: auditState.lastScan,
            description: "Auditoria contínua",
            variant: "outline",
            label: "Info"
          }
        ]
      },
      {
        type: "Alert",
        title: "Atenção de Segurança",
        description: "Detectadas portas de bancos de dados (5432, 6379) abertas em interfaces públicas sem firewall delimitador.",
        variant: "destructive"
      },
      {
        type: "Card",
        title: "Controle de Varredura e Ações",
        description: "Dispare auditorias manuais ou consulte os relatórios gerados pelo módulo WASM",
        children: [
          {
            type: "Button",
            id: "btn-run-scan",
            title: "Executar Auditoria Agora (via WASM)",
            variant: "default"
          }
        ]
      },
      {
        type: "Card",
        title: "Inventário de Portas e Riscos Detectados",
        description: "Mapeamento em tempo real correlacionando sockets abertos e serviços correspondentes",
        children: [
          {
            type: "Table",
            columns: [
              { key: "port", label: "Porta" },
              { key: "proto", label: "Protocolo" },
              { key: "service", label: "Serviço Identificado" },
              { key: "host", label: "Host / Nó" },
              { key: "risk", label: "Nível de Risco" },
              { key: "status", label: "Status" }
            ],
            rows: auditState.ports
          }
        ]
      }
    ]
  });
}

function renderUI() {
  if (self.kraken && self.kraken.surface === "widget") {
    kraken.render({
      type: "Card",
      title: "Resumo de Conformidade de Rede",
      description: "Auditoria contínua em " + auditState.scannedCount + " hosts",
      children: [
        {
          type: "StatCard",
          title: "Portas de Alto Risco",
          value: String(auditState.riskCount),
          description: "Serviços expostos (Postgres, Redis)",
          variant: "error",
          label: "Crítico"
        },
        {
          type: "Button",
          id: "btn-run-scan",
          title: "Executar Auditoria WASM",
          variant: "default"
        }
      ]
    });
    return;
  }
  renderDashboard();
}

// Handler de eventos recebidos do Host UI
kraken.on("btn-run-scan", async () => {
  kraken.log("Iniciando auditoria de portas via WASM backend...");
  
  try {
    const res = await kraken.backend.call("audit_ports", { target: "all" });
    kraken.log("Auditoria concluída com sucesso: " + JSON.stringify(res));
    
    auditState.lastScan = new Date().toLocaleTimeString();
    renderUI();
  } catch (err) {
    kraken.log("Falha ao invocar backend: " + (err.message || err));
    auditState.lastScan = "Falha (" + new Date().toLocaleTimeString() + ")";
    renderUI();
  }
});

// Renderização inicial ao carregar o plugin
renderUI();
