# Plugin: Microsoft Azure Key Vault Provider

Este plugin implementa o contrato de **Dynamic Credential Resolver / Secret Provider** do Kraken Admin Tool, permitindo autenticação federada no Microsoft Entra ID (Azure AD) e resolução *Just-in-Time* (em tempo de execução) de chaves SSH e senhas armazenadas no **Microsoft Azure Key Vault**.

---

## 1. Contrato da Arquitetura de Plugins

Um plugin de Key Vault deve declarar as permissões e implementar as ações padronizadas descritas abaixo.

### Permissões Necessárias (`plugin.json`)

```json
{
  "permissions": [
    "credentials:resolve",
    "credentials:manage",
    "http:outbound",
    "ui:sidebar"
  ],
  "allowed_hosts": [
    "login.microsoftonline.com",
    "vault.azure.net"
  ]
}
```

- `credentials:resolve`: Permite que o Kraken chame o plugin para obter chaves SSH / senhas ao conectar a um host (`vault.resolve_secret`).
- `credentials:manage`: Permite listar segredos (`vault.list_secrets`), criar segredos (`vault.put_secret`) e testar conexão (`vault.test_connection`).
- `http:outbound`: Permite chamadas HTTP externas aos endpoints da Microsoft/Azure.

---

## 2. Ações do Contrato (Actions & Payloads)

### A. `vault.resolve_secret`
Invocado automaticamente pelo Kraken ao estabelecer conexão SSH ou Docker com um host cadastrado com `credential_source: "plugin"`.

**Entrada (Payload recebido pelo Plugin):**
```json
{
  "server_id": 4,
  "server_name": "VM-Production-Web",
  "host_url": "ssh://azureuser@20.10.45.12:22",
  "ssh_user": "azureuser",
  "credential_plugin_id": "com.microsoft.azure-keyvault",
  "secret_reference": "prod-linux-bastion-key",
  "secret_version": "v1",
  "vault_config": {
    "vault_name": "corp-prod-vault",
    "tenant_id": "847291a4-9843-4e89-9a2c-contoso"
  },
  "purpose": "ssh_connect"
}
```

**Saída esperada (Retorno do Plugin):**
```json
{
  "ssh_user": "azureuser",
  "ssh_auth_method": "key",
  "ssh_private_key": "-----BEGIN OPENSSH PRIVATE KEY-----\n...",
  "ssh_key_passphrase": "",
  "sudo_password": ""
}
```

---

### B. `vault.list_secrets`
Invocado pela UI do Kraken para listar os segredos disponíveis no Key Vault ao vincular um servidor.

**Saída esperada:**
```json
{
  "secrets": [
    {
      "id": "prod-linux-bastion-key",
      "name": "prod-linux-bastion-key",
      "content_type": "ssh-private-key",
      "vault_uri": "https://corp-prod-vault.vault.azure.net/secrets/prod-linux-bastion-key",
      "enabled": true,
      "updated_at": "2026-11-02T18:30:00Z",
      "tags": {
        "env": "prod"
      }
    }
  ]
}
```

---

### C. `vault.put_secret`
Invocado para incluir uma nova chave privada ou credencial diretamente no Azure Key Vault.

**Entrada:**
```json
{
  "name": "srv-db-prod-key",
  "value": "-----BEGIN OPENSSH PRIVATE KEY-----\n...",
  "content_type": "ssh-private-key",
  "tags": { "managed_by": "kraken" }
}
```

---

### D. `vault.test_connection`
Testa a conectividade e a validade dos tokens/credenciais de acesso com a API do cofre.

**Saída esperada:**
```json
{
  "success": true,
  "message": "Autenticado com sucesso no cofre corp-prod-vault",
  "vault": "corp-prod-vault",
  "user": "admin@contoso.onmicrosoft.com"
}
```

---

## 3. Como Vincular no Cadastro de Servidores

Ao adicionar ou editar um servidor no Kraken:
1. No campo de credenciais, defina `credential_source: "plugin"`.
2. Em `credential_plugin_id`, informe `com.microsoft.azure-keyvault` (ou o ID do plugin desejado).
3. Em `secret_reference`, informe o nome ou URI do segredo (ex: `prod-linux-bastion-key`).
4. Em `vault_config`, configure opcionalmente parâmetros como `{ "vault_name": "corp-prod-vault" }`.

O Kraken **nunca gravará a chave privada no banco local**; toda conexão obterá a chave dinamicamente direto do cofre.
