package websocket

import (
	"bufio"
	"bytes"
	"context"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker/pkg/stdcopy"
	"github.com/gorilla/websocket"
	"github.com/kraken/admin-tool/backend/internal/docker"
	"github.com/kraken/admin-tool/backend/internal/k8s"
)

var (
	allowedOriginsMu sync.RWMutex
	allowedOrigins   = []string{"http://localhost:3000", "http://127.0.0.1:3000"}
)

// SetAllowedOrigins configures the allowed Origin headers for WebSocket upgrades
func SetAllowedOrigins(origins []string) {
	allowedOriginsMu.Lock()
	defer allowedOriginsMu.Unlock()
	allowedOrigins = origins
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	CheckOrigin: func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		if origin == "" {
			return true // Direct/native clients without browser Origin header
		}
		allowedOriginsMu.RLock()
		defer allowedOriginsMu.RUnlock()
		for _, o := range allowedOrigins {
			if o == "*" || strings.EqualFold(o, origin) {
				return true
			}
		}
		log.Printf("[WS] Rejected connection from unauthorized origin: %s", origin)
		return false
	},
}

// wsLineWriter implements io.Writer and sends line-buffered output to a WebSocket connection
type wsLineWriter struct {
	conn   *websocket.Conn
	mu     *sync.Mutex
	buf    bytes.Buffer
	bufMu  sync.Mutex
}

func newWSLineWriter(conn *websocket.Conn, mu *sync.Mutex) *wsLineWriter {
	return &wsLineWriter{
		conn: conn,
		mu:   mu,
	}
}

func (w *wsLineWriter) Write(p []byte) (n int, err error) {
	w.bufMu.Lock()
	defer w.bufMu.Unlock()

	n = len(p)
	w.buf.Write(p)

	for {
		line, readErr := w.buf.ReadBytes('\n')
		if readErr != nil {
			// Incomplete line left in buffer, restore it
			if len(line) > 0 {
				w.buf.Write(line)
			}
			break
		}

		cleanLine := bytes.TrimRight(line, "\r\n")
		w.mu.Lock()
		_ = w.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		err = w.conn.WriteMessage(websocket.TextMessage, cleanLine)
		w.mu.Unlock()
		if err != nil {
			return n, err
		}
	}

	return n, nil
}

func (w *wsLineWriter) Flush() error {
	w.bufMu.Lock()
	defer w.bufMu.Unlock()

	remaining := w.buf.Bytes()
	if len(remaining) > 0 {
		cleanLine := bytes.TrimRight(remaining, "\r\n")
		w.mu.Lock()
		_ = w.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		err := w.conn.WriteMessage(websocket.TextMessage, cleanLine)
		w.mu.Unlock()
		w.buf.Reset()
		return err
	}
	return nil
}

// HandleDockerLogs streams container logs over WebSocket
func HandleDockerLogs(service *docker.DockerService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		containerID := strings.TrimSpace(r.URL.Query().Get("containerId"))
		if containerID == "" || len(containerID) > 128 {
			http.Error(w, "Valid query parameter 'containerId' is required", http.StatusBadRequest)
			return
		}

		tail := r.URL.Query().Get("tail")
		if tail == "" {
			tail = "100"
		}
		follow := r.URL.Query().Get("follow") != "false"

		var serverID uint = 0
		if sID := r.URL.Query().Get("serverId"); sID != "" {
			if id, err := strconv.ParseUint(sID, 10, 32); err == nil {
				serverID = uint(id)
			}
		}

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Printf("[WS] Docker logs upgrade failed: %v", err)
			return
		}
		defer conn.Close()

		ctx, cancel := context.WithCancel(r.Context())
		defer cancel()

		var writeMu sync.Mutex

		// Detect client disconnect
		go func() {
			for {
				if _, _, err := conn.ReadMessage(); err != nil {
					cancel()
					return
				}
			}
		}()

		logStream, err := service.StreamContainerLogsOnServer(ctx, serverID, containerID, follow, tail)
		if err != nil {
			writeMu.Lock()
			_ = conn.WriteMessage(websocket.TextMessage, []byte("[Error] "+err.Error()))
			writeMu.Unlock()
			return
		}
		defer logStream.Close()

		isTTY, _ := service.IsContainerTTYOnServer(ctx, serverID, containerID)

		if isTTY {
			scanner := bufio.NewScanner(logStream)
			for scanner.Scan() {
				select {
				case <-ctx.Done():
					return
				default:
					line := scanner.Bytes()
					writeMu.Lock()
					_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
					err := conn.WriteMessage(websocket.TextMessage, line)
					writeMu.Unlock()
					if err != nil {
						return
					}
				}
			}
		} else {
			writer := newWSLineWriter(conn, &writeMu)
			// stdcopy separates stdout and stderr headers from raw multiplexed Docker streams
			_, _ = stdcopy.StdCopy(writer, writer, logStream)
			_ = writer.Flush()
		}
	}
}

// HandleK8sLogs streams Kubernetes pod logs over WebSocket
func HandleK8sLogs(service *k8s.K8sService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		podName := strings.TrimSpace(r.URL.Query().Get("pod"))
		if podName == "" {
			http.Error(w, "Query parameter 'pod' is required", http.StatusBadRequest)
			return
		}
		if err := k8s.ValidateK8sName(podName); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		namespace := strings.TrimSpace(r.URL.Query().Get("namespace"))
		if namespace == "" {
			namespace = "default"
		}
		if err := k8s.ValidateK8sNamespace(namespace); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		containerName := strings.TrimSpace(r.URL.Query().Get("container"))
		if err := k8s.ValidateContainerName(containerName); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		tailStr := r.URL.Query().Get("tail")
		tailLines, _ := strconv.ParseInt(tailStr, 10, 64)
		if tailLines <= 0 || tailLines > 100000 {
			tailLines = 100
		}
		follow := r.URL.Query().Get("follow") != "false"

		var serverID uint = 0
		if sID := r.URL.Query().Get("serverId"); sID != "" {
			if id, err := strconv.ParseUint(sID, 10, 32); err == nil {
				serverID = uint(id)
			}
		}

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Printf("[WS] K8s logs upgrade failed: %v", err)
			return
		}
		defer conn.Close()

		ctx, cancel := context.WithCancel(r.Context())
		defer cancel()

		var writeMu sync.Mutex

		// Detect client disconnect
		go func() {
			for {
				if _, _, err := conn.ReadMessage(); err != nil {
					cancel()
					return
				}
			}
		}()

		logStream, err := service.StreamPodLogsOnServer(ctx, serverID, namespace, podName, containerName, follow, tailLines)
		if err != nil {
			writeMu.Lock()
			_ = conn.WriteMessage(websocket.TextMessage, []byte("[Error] "+err.Error()))
			writeMu.Unlock()
			return
		}
		defer logStream.Close()

		scanner := bufio.NewScanner(logStream)
		for scanner.Scan() {
			select {
			case <-ctx.Done():
				return
			default:
				line := scanner.Bytes()
				writeMu.Lock()
				_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
				err := conn.WriteMessage(websocket.TextMessage, line)
				writeMu.Unlock()
				if err != nil {
					return
				}
			}
		}
	}
}
