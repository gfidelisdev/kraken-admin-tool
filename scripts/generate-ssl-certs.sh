#!/usr/bin/env bash
# ==============================================================================
# Kraken Admin Tool - Self-Signed SSL Certificate Generator
# ==============================================================================
# Generates a high-security self-signed TLS certificate (RSA 4096-bit) with
# comprehensive Subject Alternative Names (SAN) covering localhost, 127.0.0.1,
# ::1, machine hostname, and all detected local IPv4 network interfaces.
#
# Usage:
#   ./scripts/generate-ssl-certs.sh [OUTPUT_DIR] [DAYS] [EXTRA_SANS]
# Example:
#   ./scripts/generate-ssl-certs.sh ./certs 365 "kraken.local,192.168.1.50"
# ==============================================================================

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

OUTPUT_DIR="${1:-${PROJECT_ROOT}/certs}"
DAYS="${2:-365}"
EXTRA_SANS="${3:-}"

CERT_FILE="${OUTPUT_DIR}/cert.pem"
KEY_FILE="${OUTPUT_DIR}/key.pem"
CONFIG_FILE="${OUTPUT_DIR}/openssl.cnf"

echo "=== Kraken Admin Tool: SSL Certificate Generator ==="
echo "Target directory: ${OUTPUT_DIR}"
echo "Validity:         ${DAYS} days"

mkdir -p "${OUTPUT_DIR}"

if ! command -v openssl &>/dev/null; then
  echo "Error: openssl CLI is required but not installed." >&2
  exit 1
fi

# Detect local hostname and machine names
HOST_NAME="$(hostname 2>/dev/null || echo 'localhost')"
FQDN_NAME="$(hostname -f 2>/dev/null || echo "${HOST_NAME}")"

# Collect unique DNS names
declare -A DNS_NAMES
DNS_NAMES["localhost"]=1
DNS_NAMES["${HOST_NAME}"]=1
DNS_NAMES["${FQDN_NAME}"]=1
DNS_NAMES["kraken-proxy"]=1

# Collect IP addresses
declare -A IP_ADDRS
IP_ADDRS["127.0.0.1"]=1
IP_ADDRS["::1"]=1

# Detect local interface IPv4 addresses
if command -v hostname &>/dev/null && hostname -I &>/dev/null; then
  for ip in $(hostname -I); do
    if [[ "${ip}" =~ ^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
      IP_ADDRS["${ip}"]=1
    fi
  done
elif command -v ip &>/dev/null; then
  for ip in $(ip -4 addr show | awk '/inet / {print $2}' | cut -d/ -f1); do
    IP_ADDRS["${ip}"]=1
  done
fi

# Parse any additional SANS passed as argument or env var
if [[ -n "${EXTRA_SANS}" ]]; then
  IFS=',' read -ra SANS <<< "${EXTRA_SANS}"
  for san in "${SANS[@]}"; do
    san="$(echo "${san}" | xargs)"
    if [[ "${san}" =~ ^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
      IP_ADDRS["${san}"]=1
    elif [[ -n "${san}" ]]; then
      DNS_NAMES["${san}"]=1
    fi
  done
fi

# Build SAN string for OpenSSL config
SAN_ENTRIES=()
dns_idx=1
for dns in "${!DNS_NAMES[@]}"; do
  SAN_ENTRIES+=("DNS.${dns_idx} = ${dns}")
  ((dns_idx++))
done

ip_idx=1
for ip in "${!IP_ADDRS[@]}"; do
  SAN_ENTRIES+=("IP.${ip_idx} = ${ip}")
  ((ip_idx++))
done

# Write temporary OpenSSL configuration
cat > "${CONFIG_FILE}" <<EOF
[req]
default_bits        = 4096
default_md          = sha256
distinguished_name  = req_distinguished_name
req_extensions      = req_ext
x509_extensions     = v3_ca
prompt              = no

[req_distinguished_name]
C  = BR
ST = SP
L  = Local
O  = Kraken Admin Tool
OU = Development
CN = ${HOST_NAME}

[req_ext]
subjectAltName = @alt_names

[v3_ca]
subjectAltName       = @alt_names
basicConstraints     = critical, CA:TRUE
keyUsage             = critical, digitalSignature, keyEncipherment, keyCertSign
extendedKeyUsage     = serverAuth, clientAuth

[alt_names]
EOF

for entry in "${SAN_ENTRIES[@]}"; do
  echo "${entry}" >> "${CONFIG_FILE}"
done

echo "Generating RSA 4096-bit private key and self-signed certificate..."
openssl req -x509 -newkey rsa:4096 -nodes \
  -keyout "${KEY_FILE}" \
  -out "${CERT_FILE}" \
  -days "${DAYS}" \
  -config "${CONFIG_FILE}"

chmod 600 "${KEY_FILE}"
chmod 644 "${CERT_FILE}"

# Remove temporary OpenSSL config
rm -f "${CONFIG_FILE}"

echo ""
echo "SSL certificate successfully generated!"
echo "Certificate: ${CERT_FILE}"
echo "Private Key: ${KEY_FILE}"
echo ""
echo "Configured Subject Alternative Names:"
for entry in "${SAN_ENTRIES[@]}"; do
  echo "  - ${entry}"
done
echo "=========================================================="
