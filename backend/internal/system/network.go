package system

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"log"
	"math/big"
	"net"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"

	"github.com/kraken/admin-tool/backend/internal/audit"
	"github.com/kraken/admin-tool/backend/internal/auth"
	krakencrypto "github.com/kraken/admin-tool/backend/internal/crypto"
	"github.com/kraken/admin-tool/backend/internal/k8s"
	"github.com/kraken/admin-tool/backend/internal/models"
)

const (
	k8sTLSSecretName         = "kraken-tls-secret"
	k8sConfigMapName         = "kraken-config"
	k8sNginxTemplateMapName  = "kraken-nginx-template"
	k8sIngressName           = "kraken-ingress"
	k8sProxyName             = "proxy"
	k8sFrontendName          = "frontend"
	k8sBackendName           = "backend"
)

// NormalizeDomains splits, cleans, deduplicates, and validates domain names or IP addresses.
func NormalizeDomains(raw []string) ([]string, error) {
	seen := make(map[string]bool)
	var out []string

	for _, entry := range raw {
		parts := strings.FieldsFunc(entry, func(r rune) bool {
			return r == ',' || r == ';' || r == ' ' || r == '\t' || r == '\n' || r == '\r'
		})
		for _, part := range parts {
			d := strings.TrimSpace(part)
			if d == "" {
				continue
			}
			lower := strings.ToLower(d)
			lower = strings.TrimPrefix(lower, "https://")
			lower = strings.TrimPrefix(lower, "http://")
			if slashIdx := strings.IndexByte(lower, '/'); slashIdx >= 0 {
				lower = lower[:slashIdx]
			}
			// Strip :port when present (handling bracketed IPv6 or host:port)
			if strings.HasPrefix(lower, "[") {
				if h, _, err := net.SplitHostPort(lower); err == nil {
					lower = h
				} else {
					lower = strings.Trim(lower, "[]")
				}
			} else if strings.Count(lower, ":") == 1 {
				if h, _, err := net.SplitHostPort(lower); err == nil {
					lower = h
				}
			}
			lower = strings.TrimSuffix(strings.TrimSpace(lower), ".")
			if lower == "" {
				continue
			}

			if net.ParseIP(lower) == nil {
				checkHost := lower
				if strings.HasPrefix(checkHost, "*.") {
					checkHost = strings.TrimPrefix(checkHost, "*.")
				}
				if err := k8s.ValidateK8sName(checkHost); err != nil {
					return nil, fmt.Errorf("invalid domain/FQDN %q: %w", part, err)
				}
			}

			if !seen[lower] {
				seen[lower] = true
				out = append(out, lower)
			}
		}
	}

	return out, nil
}

// GenerateMultiSANCertificate generates a self-signed X.509 v3 certificate covering all supplied domains plus internal defaults.
func GenerateMultiSANCertificate(domains []string) (certPEM, keyPEM, fingerprint string, expiresAt time.Time, sanList []string, err error) {
	privKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return "", "", "", time.Time{}, nil, fmt.Errorf("failed to generate RSA private key: %w", err)
	}

	serialLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, err := rand.Int(rand.Reader, serialLimit)
	if err != nil {
		return "", "", "", time.Time{}, nil, fmt.Errorf("failed to generate certificate serial: %w", err)
	}

	dnsSeen := make(map[string]bool)
	var dnsNames []string
	ipSeen := make(map[string]bool)
	var ipAddrs []net.IP

	addDNS := func(name string) {
		name = strings.TrimSpace(strings.ToLower(name))
		if name != "" && !dnsSeen[name] {
			dnsSeen[name] = true
			dnsNames = append(dnsNames, name)
		}
	}
	addIP := func(ip net.IP) {
		if ip == nil {
			return
		}
		key := ip.String()
		if !ipSeen[key] {
			ipSeen[key] = true
			ipAddrs = append(ipAddrs, ip)
		}
	}

	commonName := ""
	for _, d := range domains {
		if ip := net.ParseIP(d); ip != nil {
			addIP(ip)
			if commonName == "" {
				commonName = d
			}
		} else {
			addDNS(d)
			if commonName == "" {
				commonName = d
			}
		}
	}

	// Always include cluster-internal and loopback SANs so internal checks continue working seamlessly
	for _, defDNS := range []string{"localhost", "proxy", "kraken-proxy", "backend", "frontend"} {
		addDNS(defDNS)
	}
	addIP(net.ParseIP("127.0.0.1"))
	addIP(net.ParseIP("::1"))

	if commonName == "" {
		commonName = "localhost"
	}

	now := time.Now().UTC()
	expiresAt = now.Add(365 * 24 * time.Hour)

	tmpl := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Country:      []string{"BR"},
			Organization: []string{"Kraken Admin Tool"},
			CommonName:   commonName,
		},
		NotBefore:             now.Add(-5 * time.Minute),
		NotAfter:              expiresAt,
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
		DNSNames:              dnsNames,
		IPAddresses:           ipAddrs,
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &privKey.PublicKey, privKey)
	if err != nil {
		return "", "", "", time.Time{}, nil, fmt.Errorf("failed to create certificate: %w", err)
	}

	certPEMBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: derBytes})
	keyPEMBytes := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(privKey)})

	sum := sha256.Sum256(derBytes)
	fingerprint = hex.EncodeToString(sum[:])

	sanList = append(sanList, dnsNames...)
	for _, ip := range ipAddrs {
		sanList = append(sanList, ip.String())
	}

	return string(certPEMBytes), string(keyPEMBytes), fingerprint, expiresAt, sanList, nil
}

// InspectCertificatePEM validates an optional cert/key pair and extracts SANs, expiration, and SHA-256 fingerprint.
func InspectCertificatePEM(certPEM, keyPEM string) (fingerprint string, expiresAt time.Time, sanList []string, err error) {
	if strings.TrimSpace(certPEM) == "" {
		return "", time.Time{}, nil, errors.New("certificate PEM is empty")
	}

	if strings.TrimSpace(keyPEM) != "" {
		if _, err := tls.X509KeyPair([]byte(certPEM), []byte(keyPEM)); err != nil {
			return "", time.Time{}, nil, fmt.Errorf("certificate and private key do not form a valid TLS keypair: %w", err)
		}
	}

	block, _ := pem.Decode([]byte(certPEM))
	if block == nil || block.Type != "CERTIFICATE" {
		return "", time.Time{}, nil, errors.New("failed to decode PEM CERTIFICATE block")
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return "", time.Time{}, nil, fmt.Errorf("failed to parse X.509 certificate: %w", err)
	}

	sum := sha256.Sum256(block.Bytes)
	fingerprint = hex.EncodeToString(sum[:])
	expiresAt = cert.NotAfter.UTC()

	for _, d := range cert.DNSNames {
		sanList = append(sanList, d)
	}
	for _, ip := range cert.IPAddresses {
		sanList = append(sanList, ip.String())
	}

	return fingerprint, expiresAt, sanList, nil
}

// BuildNginxConfigTemplate constructs the Nginx reverse proxy template with configured FQDNs and ports 80/443 + ${HTTP_PORT}/${HTTPS_PORT}.
func BuildNginxConfigTemplate(domains []string, standardHTTPS bool) string {
	serverNames := append([]string{}, domains...)
	serverNames = append(serverNames, "_")
	serverNameLine := strings.Join(serverNames, " ")

	httpRedirect := `        if ($server_port = 80) {
            return 301 https://$host$request_uri;
        }
        return 301 https://$host:${HTTPS_PORT}$request_uri;`
	if standardHTTPS && len(domains) > 0 {
		httpRedirect = `        if ($server_port = 80) {
            return 301 https://$host$request_uri;
        }
        return 301 https://$host:${HTTPS_PORT}$request_uri;`
	}

	return fmt.Sprintf(`# ==============================================================================
# Kraken Admin Tool - Nginx Reverse Proxy Configuration Template
# ==============================================================================
map $http_upgrade $connection_upgrade {
    default upgrade;
    ''      close;
}

# 1. HTTP Server: Enforce HTTPS Redirection
server {
    listen 80;
    listen [::]:80;
    listen ${HTTP_PORT};
    listen [::]:${HTTP_PORT};
    server_name %s;

    location / {
%s
    }
}

# 2. HTTPS Server: SSL Termination & Internal Routing
server {
    listen 443 ssl;
    listen [::]:443 ssl;
    listen ${HTTPS_PORT} ssl;
    listen [::]:${HTTPS_PORT} ssl;
    server_name %s;

    # TLS Certificates
    ssl_certificate /etc/nginx/certs/cert.pem;
    ssl_certificate_key /etc/nginx/certs/key.pem;

    # SSL Security Configuration
    ssl_protocols TLSv1.2 TLSv1.3;
    ssl_prefer_server_ciphers on;
    ssl_ciphers ECDHE-ECDSA-AES128-GCM-SHA256:ECDHE-RSA-AES128-GCM-SHA256:ECDHE-ECDSA-AES256-GCM-SHA384:ECDHE-RSA-AES256-GCM-SHA384:DHE-RSA-AES128-GCM-SHA256:DHE-RSA-AES256-GCM-SHA384;
    ssl_session_cache shared:SSL:10m;
    ssl_session_timeout 1d;
    ssl_session_tickets off;

    # Security Headers
    add_header Strict-Transport-Security "max-age=31536000; includeSubDomains" always;
    add_header X-Content-Type-Options "nosniff" always;
    add_header X-Frame-Options "DENY" always;
    add_header X-XSS-Protection "1; mode=block" always;
    add_header Referrer-Policy "strict-origin-when-cross-origin" always;

    # Backend API Endpoints (including WebSockets under /api/ws/)
    location /api/ {
        proxy_pass http://backend:8080/api/;
        proxy_http_version 1.1;

        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto https;
        proxy_set_header X-Forwarded-Port $server_port;

        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection $connection_upgrade;

        proxy_read_timeout 3600s;
        proxy_send_timeout 3600s;
        proxy_buffering off;
        client_max_body_size 50M;
    }

    # Backend Health Check
    location /health {
        proxy_pass http://backend:8080/health;
        proxy_http_version 1.1;

        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto https;
    }

    # Frontend (Next.js 15 UI)
    location / {
        proxy_pass http://frontend:3000;
        proxy_http_version 1.1;

        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto https;
        proxy_set_header X-Forwarded-Port $server_port;

        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection $connection_upgrade;

        proxy_read_timeout 300s;
        proxy_send_timeout 300s;
        client_max_body_size 50M;
    }
}
`, serverNameLine, httpRedirect, serverNameLine)
}

func buildAllowedOriginsFromDomains(domains []string) []string {
	seen := make(map[string]bool)
	var out []string
	add := func(o string) {
		if o != "" && !seen[o] {
			seen[o] = true
			out = append(out, o)
		}
	}

	for _, d := range domains {
		add("https://" + d)
		add("http://" + d)
		add("https://" + d + ":8443")
		add("http://" + d + ":8000")
	}
	for _, base := range []string{
		"http://localhost:3000",
		"http://127.0.0.1:3000",
		"https://localhost:8443",
		"https://localhost",
	} {
		add(base)
	}
	return out
}

// SyncNetworkConfigToKubernetes reconciles TLS Secrets, ConfigMaps, Ingress, Proxy Service, and Deployment restarts.
func SyncNetworkConfigToKubernetes(
	ctx context.Context,
	cli kubernetes.Interface,
	ns string,
	cfg *models.SystemNetworkConfig,
	domains []string,
	certPEM, keyPEM string,
	restartDeployments bool,
	backendRestartDelay time.Duration,
) (string, error) {
	if cli == nil {
		return "Kubernetes client unavailable", errors.New("kubernetes client unavailable")
	}
	if strings.TrimSpace(ns) == "" {
		ns = "default"
	}

	nowStr := time.Now().UTC().Format(time.RFC3339)

	// 1. Upsert TLS Secret (kraken-tls-secret) with both standard kubernetes.io/tls keys and cert.pem/key.pem for Nginx
	tlsSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      k8sTLSSecretName,
			Namespace: ns,
			Labels: map[string]string{
				"app.kubernetes.io/part-of": "kraken-admin-tool",
				"app.kubernetes.io/name":    k8sTLSSecretName,
			},
		},
		Type: corev1.SecretTypeTLS,
		Data: map[string][]byte{
			"tls.crt":  []byte(certPEM),
			"tls.key":  []byte(keyPEM),
			"cert.pem": []byte(certPEM),
			"key.pem":  []byte(keyPEM),
		},
	}
	if existing, err := cli.CoreV1().Secrets(ns).Get(ctx, k8sTLSSecretName, metav1.GetOptions{}); err == nil {
		tlsSecret.ResourceVersion = existing.ResourceVersion
		if _, updErr := cli.CoreV1().Secrets(ns).Update(ctx, tlsSecret, metav1.UpdateOptions{}); updErr != nil {
			return "", fmt.Errorf("failed to update Secret %s/%s: %w", ns, k8sTLSSecretName, updErr)
		}
	} else {
		if _, crtErr := cli.CoreV1().Secrets(ns).Create(ctx, tlsSecret, metav1.CreateOptions{}); crtErr != nil {
			return "", fmt.Errorf("failed to create Secret %s/%s: %w", ns, k8sTLSSecretName, crtErr)
		}
	}

	// 2. Upsert ConfigMap kraken-config
	cm, err := cli.CoreV1().ConfigMaps(ns).Get(ctx, k8sConfigMapName, metav1.GetOptions{})
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return "", fmt.Errorf("failed to read ConfigMap %s/%s: %w", ns, k8sConfigMapName, err)
		}
		cm = &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      k8sConfigMapName,
				Namespace: ns,
				Labels:    map[string]string{"app.kubernetes.io/part-of": "kraken-admin-tool"},
			},
			Data: map[string]string{},
		}
		if cm.Data == nil {
			cm.Data = map[string]string{}
		}
		cm.Data["KRAKEN_FQDNS"] = strings.Join(domains, ",")
		cm.Data["KRAKEN_INGRESS_ENABLED"] = strconv.FormatBool(cfg.IngressEnabled)
		cm.Data["CORS_ORIGINS"] = strings.Join(buildAllowedOriginsFromDomains(domains), ",")
		if _, crtErr := cli.CoreV1().ConfigMaps(ns).Create(ctx, cm, metav1.CreateOptions{}); crtErr != nil {
			return "", fmt.Errorf("failed to create ConfigMap %s/%s: %w", ns, k8sConfigMapName, crtErr)
		}
	} else {
		if cm.Data == nil {
			cm.Data = map[string]string{}
		}
		cm.Data["KRAKEN_FQDNS"] = strings.Join(domains, ",")
		cm.Data["KRAKEN_INGRESS_ENABLED"] = strconv.FormatBool(cfg.IngressEnabled)
		cm.Data["CORS_ORIGINS"] = strings.Join(buildAllowedOriginsFromDomains(domains), ",")
		if _, updErr := cli.CoreV1().ConfigMaps(ns).Update(ctx, cm, metav1.UpdateOptions{}); updErr != nil {
			return "", fmt.Errorf("failed to update ConfigMap %s/%s: %w", ns, k8sConfigMapName, updErr)
		}
	}

	// 3. Upsert ConfigMap kraken-nginx-template
	nginxRendered := BuildNginxConfigTemplate(domains, cfg.StandardHTTPSPort)
	nginxCM, err := cli.CoreV1().ConfigMaps(ns).Get(ctx, k8sNginxTemplateMapName, metav1.GetOptions{})
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return "", fmt.Errorf("failed to read ConfigMap %s/%s: %w", ns, k8sNginxTemplateMapName, err)
		}
		nginxCM = &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      k8sNginxTemplateMapName,
				Namespace: ns,
				Labels:    map[string]string{"app.kubernetes.io/part-of": "kraken-admin-tool"},
			},
			Data: map[string]string{"default.conf.template": nginxRendered},
		}
		if _, crtErr := cli.CoreV1().ConfigMaps(ns).Create(ctx, nginxCM, metav1.CreateOptions{}); crtErr != nil {
			return "", fmt.Errorf("failed to create ConfigMap %s/%s: %w", ns, k8sNginxTemplateMapName, crtErr)
		}
	} else {
		if nginxCM.Data == nil {
			nginxCM.Data = map[string]string{}
		}
		nginxCM.Data["default.conf.template"] = nginxRendered
		if _, updErr := cli.CoreV1().ConfigMaps(ns).Update(ctx, nginxCM, metav1.UpdateOptions{}); updErr != nil {
			return "", fmt.Errorf("failed to update ConfigMap %s/%s: %w", ns, k8sNginxTemplateMapName, updErr)
		}
	}

	// 4. Reconcile Kubernetes Ingress (kraken-ingress)
	ingressAction := "removed"
	if cfg.IngressEnabled && len(domains) > 0 {
		pathTypePrefix := networkingv1.PathTypePrefix
		rules := make([]networkingv1.IngressRule, 0, len(domains))
		for _, host := range domains {
			rules = append(rules, networkingv1.IngressRule{
				Host: host,
				IngressRuleValue: networkingv1.IngressRuleValue{
					HTTP: &networkingv1.HTTPIngressRuleValue{
						Paths: []networkingv1.HTTPIngressPath{
							{
								Path:     "/api",
								PathType: &pathTypePrefix,
								Backend: networkingv1.IngressBackend{
									Service: &networkingv1.IngressServiceBackend{
										Name: k8sBackendName,
										Port: networkingv1.ServiceBackendPort{Number: 8080},
									},
								},
							},
							{
								Path:     "/health",
								PathType: &pathTypePrefix,
								Backend: networkingv1.IngressBackend{
									Service: &networkingv1.IngressServiceBackend{
										Name: k8sBackendName,
										Port: networkingv1.ServiceBackendPort{Number: 8080},
									},
								},
							},
							{
								Path:     "/",
								PathType: &pathTypePrefix,
								Backend: networkingv1.IngressBackend{
									Service: &networkingv1.IngressServiceBackend{
										Name: k8sFrontendName,
										Port: networkingv1.ServiceBackendPort{Number: 3000},
									},
								},
							},
						},
					},
				},
			})
		}

		annotations := map[string]string{
			"nginx.ingress.kubernetes.io/ssl-redirect":       "true",
			"nginx.ingress.kubernetes.io/force-ssl-redirect": "true",
			"nginx.ingress.kubernetes.io/proxy-read-timeout": "3600",
			"nginx.ingress.kubernetes.io/proxy-send-timeout": "3600",
			"nginx.ingress.kubernetes.io/proxy-body-size":    "50m",
			"nginx.ingress.kubernetes.io/websocket-services": k8sBackendName,
		}
		if issuer := strings.TrimSpace(cfg.CertIssuer); issuer != "" {
			annotations["cert-manager.io/cluster-issuer"] = issuer
		}

		ing := &networkingv1.Ingress{
			ObjectMeta: metav1.ObjectMeta{
				Name:        k8sIngressName,
				Namespace:   ns,
				Labels:      map[string]string{"app.kubernetes.io/part-of": "kraken-admin-tool", "app.kubernetes.io/name": k8sIngressName},
				Annotations: annotations,
			},
			Spec: networkingv1.IngressSpec{
				TLS: []networkingv1.IngressTLS{
					{
						Hosts:      domains,
						SecretName: k8sTLSSecretName,
					},
				},
				Rules: rules,
			},
		}
		if className := strings.TrimSpace(cfg.IngressClassName); className != "" {
			ing.Spec.IngressClassName = &className
		}

		if existingIng, err := cli.NetworkingV1().Ingresses(ns).Get(ctx, k8sIngressName, metav1.GetOptions{}); err == nil {
			ing.ResourceVersion = existingIng.ResourceVersion
			if _, updErr := cli.NetworkingV1().Ingresses(ns).Update(ctx, ing, metav1.UpdateOptions{}); updErr != nil {
				return "", fmt.Errorf("failed to update Ingress %s/%s: %w", ns, k8sIngressName, updErr)
			}
		} else {
			if _, crtErr := cli.NetworkingV1().Ingresses(ns).Create(ctx, ing, metav1.CreateOptions{}); crtErr != nil {
				return "", fmt.Errorf("failed to create Ingress %s/%s: %w", ns, k8sIngressName, crtErr)
			}
		}
		ingressAction = fmt.Sprintf("configured for %s (TLS port 443)", strings.Join(domains, ", "))
	} else {
		if delErr := cli.NetworkingV1().Ingresses(ns).Delete(ctx, k8sIngressName, metav1.DeleteOptions{}); delErr != nil && !apierrors.IsNotFound(delErr) {
			return "", fmt.Errorf("failed to remove Ingress %s/%s: %w", ns, k8sIngressName, delErr)
		}
	}

	// 5. Update Service/proxy to ensure standard ports 80 and 443 are exposed alongside 8000 and 8443
	if proxySvc, err := cli.CoreV1().Services(ns).Get(ctx, k8sProxyName, metav1.GetOptions{}); err == nil {
		ensureServicePort := func(name string, port int32) {
			for i := range proxySvc.Spec.Ports {
				if proxySvc.Spec.Ports[i].Port == port || proxySvc.Spec.Ports[i].Name == name {
					return
				}
			}
			proxySvc.Spec.Ports = append(proxySvc.Spec.Ports, corev1.ServicePort{
				Name:       name,
				Port:       port,
				TargetPort: intstr.FromInt(int(port)),
				Protocol:   corev1.ProtocolTCP,
			})
		}
		ensureServicePort("http-std", 80)
		ensureServicePort("https-std", 443)
		ensureServicePort("http", 8000)
		ensureServicePort("https", 8443)
		if _, updErr := cli.CoreV1().Services(ns).Update(ctx, proxySvc, metav1.UpdateOptions{}); updErr != nil {
			log.Printf("[SYSTEM-NET] Warning: failed to update Service %s/%s ports: %v", ns, k8sProxyName, updErr)
		}
	}

	// 6. Update Deployment/proxy to mount kraken-tls-secret and trigger rolling restarts
	restarted := []string{}
	if proxyDep, err := cli.AppsV1().Deployments(ns).Get(ctx, k8sProxyName, metav1.GetOptions{}); err == nil {
		// Switch tls-certs volume from emptyDir to Secret kraken-tls-secret
		replacedVol := false
		for i := range proxyDep.Spec.Template.Spec.Volumes {
			if proxyDep.Spec.Template.Spec.Volumes[i].Name == "tls-certs" {
				proxyDep.Spec.Template.Spec.Volumes[i].VolumeSource = corev1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{
						SecretName: k8sTLSSecretName,
					},
				}
				replacedVol = true
				break
			}
		}
		if !replacedVol {
			proxyDep.Spec.Template.Spec.Volumes = append(proxyDep.Spec.Template.Spec.Volumes, corev1.Volume{
				Name: "tls-certs",
				VolumeSource: corev1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{
						SecretName: k8sTLSSecretName,
					},
				},
			})
		}

		// Remove the self-signed initContainer since tls-certs is now managed via kraken-tls-secret
		if len(proxyDep.Spec.Template.Spec.InitContainers) > 0 {
			filtered := make([]corev1.Container, 0, len(proxyDep.Spec.Template.Spec.InitContainers))
			for _, ic := range proxyDep.Spec.Template.Spec.InitContainers {
				if ic.Name != "generate-tls-certs" {
					filtered = append(filtered, ic)
				}
			}
			proxyDep.Spec.Template.Spec.InitContainers = filtered
		}

		// Ensure ports 80 and 443 exist on proxy container
		for i := range proxyDep.Spec.Template.Spec.Containers {
			if proxyDep.Spec.Template.Spec.Containers[i].Name == k8sProxyName {
				ensureContainerPort(&proxyDep.Spec.Template.Spec.Containers[i], "http-std", 80)
				ensureContainerPort(&proxyDep.Spec.Template.Spec.Containers[i], "https-std", 443)
			}
		}

		if proxyDep.Spec.Template.Annotations == nil {
			proxyDep.Spec.Template.Annotations = make(map[string]string)
		}
		proxyDep.Spec.Template.Annotations["kraken.io/cert-fingerprint"] = cfg.CertFingerprint
		if restartDeployments {
			proxyDep.Spec.Template.Annotations["kubectl.kubernetes.io/restartedAt"] = nowStr
		}
		if _, updErr := cli.AppsV1().Deployments(ns).Update(ctx, proxyDep, metav1.UpdateOptions{}); updErr != nil {
			return "", fmt.Errorf("failed to update Deployment %s/%s: %w", ns, k8sProxyName, updErr)
		}
		if restartDeployments {
			restarted = append(restarted, k8sProxyName)
		}
	}

	if restartDeployments {
		if frontDep, err := cli.AppsV1().Deployments(ns).Get(ctx, k8sFrontendName, metav1.GetOptions{}); err == nil {
			if frontDep.Spec.Template.Annotations == nil {
				frontDep.Spec.Template.Annotations = make(map[string]string)
			}
			frontDep.Spec.Template.Annotations["kubectl.kubernetes.io/restartedAt"] = nowStr
			if _, updErr := cli.AppsV1().Deployments(ns).Update(ctx, frontDep, metav1.UpdateOptions{}); updErr != nil {
				log.Printf("[SYSTEM-NET] Warning: failed to rollout-restart Deployment %s/%s: %v", ns, k8sFrontendName, updErr)
			} else {
				restarted = append(restarted, k8sFrontendName)
			}
		}

		// Restart backend asynchronously after a brief delay so the HTTP response finishes cleanly first
		restartBackend := func() {
			bgCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if backDep, err := cli.AppsV1().Deployments(ns).Get(bgCtx, k8sBackendName, metav1.GetOptions{}); err == nil {
				if backDep.Spec.Template.Annotations == nil {
					backDep.Spec.Template.Annotations = make(map[string]string)
				}
				backDep.Spec.Template.Annotations["kubectl.kubernetes.io/restartedAt"] = nowStr
				if _, updErr := cli.AppsV1().Deployments(ns).Update(bgCtx, backDep, metav1.UpdateOptions{}); updErr != nil {
					log.Printf("[SYSTEM-NET] Warning: failed to rollout-restart Deployment %s/%s: %v", ns, k8sBackendName, updErr)
				}
			}
		}
		if backendRestartDelay > 0 {
			go func() {
				time.Sleep(backendRestartDelay)
				restartBackend()
			}()
		} else {
			restartBackend()
		}
		restarted = append(restarted, k8sBackendName)
	}

	summary := fmt.Sprintf("TLS Secret '%s' and ConfigMaps updated; Ingress %s", k8sTLSSecretName, ingressAction)
	if len(restarted) > 0 {
		summary += fmt.Sprintf("; rolling restart triggered for [%s]", strings.Join(restarted, ", "))
	}
	return summary, nil
}

func ensureContainerPort(c *corev1.Container, name string, port int32) {
	for _, p := range c.Ports {
		if p.ContainerPort == port || p.Name == name {
			return
		}
	}
	c.Ports = append(c.Ports, corev1.ContainerPort{
		Name:          name,
		ContainerPort: port,
		Protocol:      corev1.ProtocolTCP,
	})
}

func (h *Handler) resolveK8sClient() (kubernetes.Interface, string, bool) {
	h.clientMu.RLock()
	customCli := h.k8sClient
	customNs := h.k8sNamespace
	h.clientMu.RUnlock()

	if customCli != nil {
		if customNs == "" {
			customNs = "default"
		}
		return customCli, customNs, true
	}
	if h.K8s != nil && h.K8s.IsConnected() {
		return h.K8s.GetClient(), h.K8s.GetCurrentNamespace(), true
	}
	return nil, "", false
}

func (h *Handler) detectRuntimeMode() string {
	mode := strings.ToLower(strings.TrimSpace(os.Getenv("KRAKEN_RUNTIME_MODE")))
	if mode == "kubernetes" || mode == "k8s" || strings.TrimSpace(os.Getenv("KUBERNETES_SERVICE_HOST")) != "" {
		return "kubernetes"
	}
	if h.K8s != nil && h.K8s.IsInCluster() {
		return "kubernetes"
	}
	return "docker"
}

// ApplySystemTimeConfig applies the configured IANA timezone to the Go process and updates the TOTP clock offset.
func ApplySystemTimeConfig(tz string, offsetSeconds int64) error {
	cleanTZ := strings.TrimSpace(tz)
	if cleanTZ != "" {
		loc, err := time.LoadLocation(cleanTZ)
		if err != nil {
			return fmt.Errorf("invalid timezone %q: %w", cleanTZ, err)
		}
		time.Local = loc
		_ = os.Setenv("TZ", cleanTZ)
	}
	auth.SetTOTPClockOffsetSeconds(offsetSeconds)
	return nil
}

// EnsureBootNetworkConfig initializes persisted network/TLS state on first startup and keeps CORS/WS origins aligned.
func (h *Handler) EnsureBootNetworkConfig(ctx context.Context) {
	if h.DBHandle == nil || h.DBHandle.DB == nil {
		return
	}

	var cfg models.SystemNetworkConfig
	err := h.DBHandle.DB.Order("id asc").First(&cfg).Error
	if err == nil {
		if applyErr := ApplySystemTimeConfig(cfg.Timezone, cfg.TOTPClockOffsetSeconds); applyErr != nil {
			log.Printf("[SYSTEM-TIME] Warning: failed to apply persisted time config: %v", applyErr)
		}
		var domains []string
		if cfg.DomainsJSON != "" {
			_ = json.Unmarshal([]byte(cfg.DomainsJSON), &domains)
		}
		h.notifyOriginsUpdated(domains)
		return
	}

	// Bootstrap initial network config from environment if no record exists yet
	envDomains, _ := NormalizeDomains([]string{os.Getenv("KRAKEN_FQDNS")})
	ingressEnabled := strings.EqualFold(strings.TrimSpace(os.Getenv("KRAKEN_INGRESS_ENABLED")), "true")
	if len(envDomains) == 0 && !ingressEnabled && h.detectRuntimeMode() != "kubernetes" {
		return
	}

	certPEM, keyPEM, fp, expiresAt, _, genErr := GenerateMultiSANCertificate(envDomains)
	if genErr != nil {
		log.Printf("[SYSTEM-NET] Warning: failed to generate boot TLS certificate: %v", genErr)
		return
	}

	encKey, encErr := krakencrypto.Encrypt(keyPEM)
	if encErr != nil {
		encKey = keyPEM
	}
	domainsBytes, _ := json.Marshal(envDomains)
	now := time.Now().UTC()

	cfg = models.SystemNetworkConfig{
		IngressEnabled:    ingressEnabled,
		DomainsJSON:       string(domainsBytes),
		StandardHTTPSPort: true,
		TLSMode:           "auto_self_signed",
		CertPEM:           certPEM,
		KeyPEM:            encKey,
		CertFingerprint:   fp,
		CertExpiresAt:     &expiresAt,
		UpdatedBy:         "system",
		UpdatedAt:         now,
	}

	if cli, ns, connected := h.resolveK8sClient(); connected {
		msg, syncErr := SyncNetworkConfigToKubernetes(ctx, cli, ns, &cfg, envDomains, certPEM, keyPEM, false, h.backendRestartDelay)
		cfg.LastAppliedAt = &now
		if syncErr != nil {
			cfg.LastApplyStatus = "error"
			cfg.LastApplyMessage = syncErr.Error()
		} else {
			cfg.LastApplyStatus = "success"
			cfg.LastApplyMessage = msg
		}
	}

	_ = h.DBHandle.DB.Create(&cfg).Error
	h.notifyOriginsUpdated(envDomains)
}

func (h *Handler) notifyOriginsUpdated(domains []string) {
	h.clientMu.RLock()
	fn := h.onOriginsUpdated
	h.clientMu.RUnlock()
	if fn != nil {
		fn(buildAllowedOriginsFromDomains(domains))
	}
}

func (h *Handler) buildNetworkConfigResponse(cfg *models.SystemNetworkConfig) *models.NetworkConfigResponse {
	var domains []string
	if cfg != nil && cfg.DomainsJSON != "" {
		_ = json.Unmarshal([]byte(cfg.DomainsJSON), &domains)
	}
	if domains == nil {
		domains = []string{}
	}

	var certSANs []string
	if cfg != nil && strings.TrimSpace(cfg.CertPEM) != "" {
		if _, _, sans, err := InspectCertificatePEM(cfg.CertPEM, ""); err == nil {
			certSANs = sans
		}
	}

	_, ns, k8sConnected := h.resolveK8sClient()
	resp := &models.NetworkConfigResponse{
		Domains:                domains,
		StandardHTTPSPort:      true,
		TLSMode:                "auto_self_signed",
		CertSANs:               certSANs,
		Timezone:               time.Local.String(),
		TOTPClockOffsetSeconds: auth.GetTOTPClockOffsetSeconds(),
		ServerUnix:             time.Now().Unix(),
		RuntimeMode:            h.detectRuntimeMode(),
		K8sConnected:           k8sConnected,
		K8sNamespace:           ns,
	}
	if cfg != nil && cfg.ID != 0 {
		resp.IngressEnabled = cfg.IngressEnabled
		resp.IngressClassName = cfg.IngressClassName
		resp.StandardHTTPSPort = cfg.StandardHTTPSPort
		if cfg.TLSMode != "" {
			resp.TLSMode = cfg.TLSMode
		}
		if cfg.Timezone != "" {
			resp.Timezone = cfg.Timezone
		}
		resp.TOTPClockOffsetSeconds = cfg.TOTPClockOffsetSeconds
		resp.CertIssuer = cfg.CertIssuer
		resp.CertPEM = cfg.CertPEM
		resp.CertFingerprint = cfg.CertFingerprint
		resp.CertExpiresAt = cfg.CertExpiresAt
		resp.LastAppliedAt = cfg.LastAppliedAt
		resp.LastApplyStatus = cfg.LastApplyStatus
		resp.LastApplyMessage = cfg.LastApplyMessage
		resp.UpdatedBy = cfg.UpdatedBy
		t := cfg.UpdatedAt
		resp.UpdatedAt = &t
	}
	return resp
}

// ApplyNetworkConfig validates domains, manages TLS certificates, saves configuration, and reconciles Kubernetes resources.
func (h *Handler) ApplyNetworkConfig(
	ctx context.Context,
	req models.UpdateNetworkConfigRequest,
	actor string,
	userID uint,
) (*models.NetworkConfigResponse, error) {
	h.netMu.Lock()
	defer h.netMu.Unlock()

	domains, err := NormalizeDomains(req.Domains)
	if err != nil {
		return nil, err
	}
	if req.IngressEnabled && len(domains) == 0 {
		return nil, errors.New("at least one valid DNS name / FQDN is required when Ingress is enabled")
	}

	var cfg models.SystemNetworkConfig
	if h.DBHandle != nil && h.DBHandle.DB != nil {
		_ = h.DBHandle.DB.Order("id asc").First(&cfg).Error
	}

	var prevDomains []string
	if cfg.DomainsJSON != "" {
		_ = json.Unmarshal([]byte(cfg.DomainsJSON), &prevDomains)
	}

	tlsMode := strings.ToLower(strings.TrimSpace(req.TLSMode))
	if tlsMode == "" {
		tlsMode = "auto_self_signed"
	}
	if tlsMode != "auto_self_signed" && tlsMode != "custom" {
		return nil, fmt.Errorf("invalid tls_mode %q: must be 'auto_self_signed' or 'custom'", req.TLSMode)
	}

	standardHTTPS := true
	if req.StandardHTTPSPort != nil {
		standardHTTPS = *req.StandardHTTPSPort
	}
	restartDeployments := true
	if req.RestartDeployments != nil {
		restartDeployments = *req.RestartDeployments
	}

	var certPEM, keyPEM, fingerprint string
	var expiresAt time.Time

	if tlsMode == "custom" {
		certPEM = strings.TrimSpace(req.CustomCertPEM)
		keyPEM = strings.TrimSpace(req.CustomKeyPEM)
		if certPEM == "" && cfg.CertPEM != "" && keyPEM == "" && cfg.KeyPEM != "" {
			certPEM = cfg.CertPEM
			decrypted, decErr := krakencrypto.Decrypt(cfg.KeyPEM)
			if decErr == nil {
				keyPEM = decrypted
			}
		}
		if certPEM == "" || keyPEM == "" {
			return nil, errors.New("custom TLS mode requires both PEM certificate and PEM private key")
		}
		fp, exp, _, inspErr := InspectCertificatePEM(certPEM, keyPEM)
		if inspErr != nil {
			return nil, inspErr
		}
		fingerprint = fp
		expiresAt = exp
	} else {
		needsNewCert := strings.TrimSpace(cfg.CertPEM) == "" ||
			strings.TrimSpace(cfg.KeyPEM) == "" ||
			cfg.TLSMode != "auto_self_signed" ||
			!sameStringSet(prevDomains, domains)

		if needsNewCert {
			cPEM, kPEM, fp, exp, _, genErr := GenerateMultiSANCertificate(domains)
			if genErr != nil {
				return nil, genErr
			}
			certPEM = cPEM
			keyPEM = kPEM
			fingerprint = fp
			expiresAt = exp
		} else {
			certPEM = cfg.CertPEM
			decrypted, decErr := krakencrypto.Decrypt(cfg.KeyPEM)
			if decErr != nil {
				cPEM, kPEM, fp, exp, _, genErr := GenerateMultiSANCertificate(domains)
				if genErr != nil {
					return nil, genErr
				}
				certPEM = cPEM
				keyPEM = kPEM
				fingerprint = fp
				expiresAt = exp
			} else {
				keyPEM = decrypted
				fingerprint = cfg.CertFingerprint
				if cfg.CertExpiresAt != nil {
					expiresAt = *cfg.CertExpiresAt
				}
			}
		}
	}

	encKey, encErr := krakencrypto.Encrypt(keyPEM)
	if encErr != nil {
		encKey = keyPEM
	}

	domainsBytes, _ := json.Marshal(domains)
	now := time.Now().UTC()

	cfg.IngressEnabled = req.IngressEnabled
	cfg.DomainsJSON = string(domainsBytes)
	cfg.IngressClassName = strings.TrimSpace(req.IngressClassName)
	cfg.StandardHTTPSPort = standardHTTPS
	cfg.TLSMode = tlsMode
	cfg.CertIssuer = strings.TrimSpace(req.CertIssuer)
	cfg.CertPEM = certPEM
	cfg.KeyPEM = encKey
	cfg.CertFingerprint = fingerprint
	cfg.CertExpiresAt = &expiresAt
	cfg.UpdatedBy = actor
	cfg.UpdatedAt = now

	if cli, ns, connected := h.resolveK8sClient(); connected {
		msg, syncErr := SyncNetworkConfigToKubernetes(ctx, cli, ns, &cfg, domains, certPEM, keyPEM, restartDeployments, h.backendRestartDelay)
		cfg.LastAppliedAt = &now
		if syncErr != nil {
			cfg.LastApplyStatus = "error"
			cfg.LastApplyMessage = syncErr.Error()
		} else {
			cfg.LastApplyStatus = "success"
			cfg.LastApplyMessage = msg
		}
	} else {
		cfg.LastAppliedAt = &now
		cfg.LastApplyStatus = "saved_local"
		cfg.LastApplyMessage = "Configuration and TLS certificate saved. Kubernetes cluster client is not connected in this runtime."
	}

	if h.DBHandle != nil && h.DBHandle.DB != nil {
		if cfg.ID == 0 {
			if err := h.DBHandle.DB.Create(&cfg).Error; err != nil {
				return nil, fmt.Errorf("failed to persist network configuration: %w", err)
			}
		} else {
			if err := h.DBHandle.DB.Save(&cfg).Error; err != nil {
				return nil, fmt.Errorf("failed to update network configuration: %w", err)
			}
		}
		audit.Log(h.DBHandle.DB, userID, actor, "SYSTEM_NETWORK_UPDATE", "network", strings.Join(domains, ","),
			fmt.Sprintf("Ingress=%v, Domains=[%s], Status=%s", cfg.IngressEnabled, strings.Join(domains, ", "), cfg.LastApplyStatus), "SUCCESS")
	}

	h.notifyOriginsUpdated(domains)

	return h.buildNetworkConfigResponse(&cfg), nil
}

func sameStringSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	ca := append([]string{}, a...)
	cb := append([]string{}, b...)
	sort.Strings(ca)
	sort.Strings(cb)
	for i := range ca {
		if ca[i] != cb[i] {
			return false
		}
	}
	return true
}

// GetNetworkConfigHandler handles GET /api/system/network-config
func (h *Handler) GetNetworkConfigHandler(w http.ResponseWriter, r *http.Request) {
	var cfg models.SystemNetworkConfig
	if h.DBHandle != nil && h.DBHandle.DB != nil {
		_ = h.DBHandle.DB.Order("id asc").First(&cfg).Error
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(h.buildNetworkConfigResponse(&cfg))
}

// UpdateNetworkConfigHandler handles PUT /api/system/network-config
func (h *Handler) UpdateNetworkConfigHandler(w http.ResponseWriter, r *http.Request) {
	var req models.UpdateNetworkConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Invalid request payload"})
		return
	}

	actor := "admin"
	var userID uint
	if claims := auth.GetUserFromContext(r.Context()); claims != nil {
		actor = claims.Username
		userID = claims.UserID
	}

	resp, err := h.ApplyNetworkConfig(r.Context(), req, actor, userID)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// RegenerateCertificateHandler handles POST /api/system/network-config/regenerate-cert
func (h *Handler) RegenerateCertificateHandler(w http.ResponseWriter, r *http.Request) {
	var cfg models.SystemNetworkConfig
	if h.DBHandle != nil && h.DBHandle.DB != nil {
		_ = h.DBHandle.DB.Order("id asc").First(&cfg).Error
	}

	var domains []string
	if cfg.DomainsJSON != "" {
		_ = json.Unmarshal([]byte(cfg.DomainsJSON), &domains)
	}

	// Force regeneration by clearing existing CertPEM before calling ApplyNetworkConfig
	h.netMu.Lock()
	if h.DBHandle != nil && h.DBHandle.DB != nil && cfg.ID != 0 {
		h.DBHandle.DB.Model(&cfg).Update("cert_pem", "")
	}
	h.netMu.Unlock()

	actor := "admin"
	var userID uint
	if claims := auth.GetUserFromContext(r.Context()); claims != nil {
		actor = claims.Username
		userID = claims.UserID
	}

	standardHTTPS := true
	if cfg.ID != 0 {
		standardHTTPS = cfg.StandardHTTPSPort
	}
	restart := true
	resp, err := h.ApplyNetworkConfig(r.Context(), models.UpdateNetworkConfigRequest{
		IngressEnabled:     cfg.IngressEnabled,
		Domains:            domains,
		IngressClassName:   cfg.IngressClassName,
		StandardHTTPSPort:  &standardHTTPS,
		TLSMode:            "auto_self_signed",
		CertIssuer:         cfg.CertIssuer,
		RestartDeployments: &restart,
	}, actor, userID)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// ApplyTimeConfig validates, persists, and applies the system timezone and optional TOTP clock offset.
func (h *Handler) ApplyTimeConfig(tz string, explicitOffset *int64, clientUnixMs int64, actor string, userID uint) (*models.NetworkConfigResponse, error) {
	h.netMu.Lock()
	defer h.netMu.Unlock()

	cleanTZ := strings.TrimSpace(tz)
	if cleanTZ != "" {
		if _, err := time.LoadLocation(cleanTZ); err != nil {
			return nil, fmt.Errorf("invalid timezone %q: %w", cleanTZ, err)
		}
	}

	var cfg models.SystemNetworkConfig
	if h.DBHandle != nil && h.DBHandle.DB != nil {
		_ = h.DBHandle.DB.Order("id asc").First(&cfg).Error
	}

	offset := cfg.TOTPClockOffsetSeconds
	if explicitOffset != nil {
		offset = *explicitOffset
	} else if clientUnixMs > 0 {
		diff := (clientUnixMs / 1000) - time.Now().Unix()
		if diff > 30 || diff < -30 {
			offset = diff
		} else {
			offset = 0
		}
	}

	if err := ApplySystemTimeConfig(cleanTZ, offset); err != nil {
		return nil, err
	}

	if cleanTZ != "" {
		cfg.Timezone = cleanTZ
	}
	cfg.TOTPClockOffsetSeconds = offset
	if actor != "" {
		cfg.UpdatedBy = actor
	}
	cfg.UpdatedAt = time.Now().UTC()

	if h.DBHandle != nil && h.DBHandle.DB != nil {
		if cfg.ID == 0 {
			if cfg.TLSMode == "" {
				cfg.TLSMode = "auto_self_signed"
			}
			cfg.StandardHTTPSPort = true
			if err := h.DBHandle.DB.Create(&cfg).Error; err != nil {
				return nil, fmt.Errorf("failed to persist time config: %w", err)
			}
		} else {
			if err := h.DBHandle.DB.Save(&cfg).Error; err != nil {
				return nil, fmt.Errorf("failed to update time config: %w", err)
			}
		}
		if userID > 0 {
			audit.Log(h.DBHandle.DB, userID, actor, "UPDATE_TIME_CONFIG", "system", "time", fmt.Sprintf("Timezone=%q, TOTPClockOffsetSeconds=%d", cfg.Timezone, cfg.TOTPClockOffsetSeconds), "SUCCESS")
		}
	}

	return h.buildNetworkConfigResponse(&cfg), nil
}

type updateTimeConfigRequest struct {
	Timezone               string `json:"timezone,omitempty"`
	TOTPClockOffsetSeconds *int64 `json:"totp_clock_offset_seconds,omitempty"`
	ClientUnixMs           int64  `json:"client_unix_ms,omitempty"`
}

// UpdateTimeConfigHandler handles PUT /api/system/time-config
func (h *Handler) UpdateTimeConfigHandler(w http.ResponseWriter, r *http.Request) {
	var req updateTimeConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Invalid request payload"})
		return
	}

	actor := "admin"
	var userID uint
	if claims := auth.GetUserFromContext(r.Context()); claims != nil {
		actor = claims.Username
		userID = claims.UserID
	}

	resp, err := h.ApplyTimeConfig(req.Timezone, req.TOTPClockOffsetSeconds, req.ClientUnixMs, actor, userID)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}
