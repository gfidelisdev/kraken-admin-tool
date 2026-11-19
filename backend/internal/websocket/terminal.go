package websocket

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/kraken/admin-tool/backend/internal/audit"
	"github.com/kraken/admin-tool/backend/internal/auth"
	"github.com/kraken/admin-tool/backend/internal/docker"
	"github.com/kraken/admin-tool/backend/internal/k8s"
	"gorm.io/gorm"
)

// terminalClientMessage is sent by the browser terminal:
// {"type":"input","data":"ls\r"} or {"type":"resize","cols":120,"rows":40}
type terminalClientMessage struct {
	Type string `json:"type"`
	Data string `json:"data,omitempty"`
	Cols uint16 `json:"cols,omitempty"`
	Rows uint16 `json:"rows,omitempty"`
}

// terminalServerMessage carries control events; terminal output itself is sent as binary frames
type terminalServerMessage struct {
	Type    string `json:"type"` // "exit" | "error"
	Code    int    `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}

// wsBinaryWriter forwards raw terminal output as binary WebSocket frames
type wsBinaryWriter struct {
	conn *websocket.Conn
	mu   *sync.Mutex
}

func (w *wsBinaryWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	_ = w.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	if err := w.conn.WriteMessage(websocket.BinaryMessage, p); err != nil {
		return 0, err
	}
	return len(p), nil
}

func parseTerminalSize(r *http.Request) docker.TerminalSize {
	size := docker.TerminalSize{Cols: 80, Rows: 24}
	if c, err := strconv.ParseUint(r.URL.Query().Get("cols"), 10, 16); err == nil && c > 0 {
		size.Cols = uint16(c)
	}
	if rows, err := strconv.ParseUint(r.URL.Query().Get("rows"), 10, 16); err == nil && rows > 0 {
		size.Rows = uint16(rows)
	}
	return size
}

func writeAuthError(w http.ResponseWriter, err error) {
	status := http.StatusForbidden
	if errors.Is(err, auth.ErrUnauthorized) {
		status = http.StatusUnauthorized
	}
	http.Error(w, err.Error(), status)
}

// runTerminal upgrades the request and bridges the WebSocket to an interactive process started by run
func runTerminal(w http.ResponseWriter, r *http.Request, run func(ctx context.Context, streams docker.TerminalStreams) (int, error)) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[WS] Terminal upgrade failed: %v", err)
		return
	}
	defer conn.Close()

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	var writeMu sync.Mutex
	stdinR, stdinW := io.Pipe()
	resizeCh := make(chan docker.TerminalSize, 8)
	resizeCh <- parseTerminalSize(r)

	go func() {
		defer cancel()
		defer stdinW.Close()
		for {
			msgType, payload, err := conn.ReadMessage()
			if err != nil {
				return
			}
			if msgType == websocket.BinaryMessage {
				if _, err := stdinW.Write(payload); err != nil {
					return
				}
				continue
			}
			var msg terminalClientMessage
			if json.Unmarshal(payload, &msg) != nil {
				continue
			}
			switch msg.Type {
			case "input":
				if _, err := stdinW.Write([]byte(msg.Data)); err != nil {
					return
				}
			case "resize":
				if msg.Cols == 0 || msg.Rows == 0 {
					continue
				}
				select {
				case resizeCh <- docker.TerminalSize{Cols: msg.Cols, Rows: msg.Rows}:
				default:
				}
			}
		}
	}()

	code, runErr := run(ctx, docker.TerminalStreams{
		Stdin:  stdinR,
		Stdout: &wsBinaryWriter{conn: conn, mu: &writeMu},
		Resize: resizeCh,
	})
	_ = stdinR.Close()

	final := terminalServerMessage{Type: "exit", Code: code}
	if runErr != nil && !errors.Is(runErr, context.Canceled) {
		final = terminalServerMessage{Type: "error", Message: runErr.Error()}
	}
	if payload, err := json.Marshal(final); err == nil {
		writeMu.Lock()
		_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		_ = conn.WriteMessage(websocket.TextMessage, payload)
		_ = conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
		writeMu.Unlock()
	}
}

func auditExec(db *gorm.DB, r *http.Request, action, resourceType, target, details string) {
	if user := auth.GetUserFromContext(r.Context()); user != nil {
		audit.Log(db, user.UserID, user.Username, action, resourceType, target, details, "SUCCESS")
	}
}

// HandleDockerExec opens an interactive shell inside a Docker container over WebSocket
func HandleDockerExec(service *docker.DockerService, db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		containerID := strings.TrimSpace(r.URL.Query().Get("containerId"))
		if containerID == "" || len(containerID) > 128 {
			http.Error(w, "Valid query parameter 'containerId' is required", http.StatusBadRequest)
			return
		}
		cmd, err := docker.ResolveShellCommand(r.URL.Query().Get("shell"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		serverID, err := auth.ResolveAuthorizedServerID(db, r)
		if err != nil {
			writeAuthError(w, err)
			return
		}

		auditExec(db, r, "EXEC_CONTAINER", "docker_container", containerID, fmt.Sprintf("Interactive shell %v on server %d", cmd, serverID))
		runTerminal(w, r, func(ctx context.Context, streams docker.TerminalStreams) (int, error) {
			return service.ExecTTYOnServer(ctx, serverID, containerID, cmd, streams)
		})
	}
}

// HandleK8sExec opens an interactive shell inside a Kubernetes pod container over WebSocket
func HandleK8sExec(service *k8s.K8sService, db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		podName := strings.TrimSpace(r.URL.Query().Get("pod"))
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
		cmd, err := docker.ResolveShellCommand(r.URL.Query().Get("shell"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		serverID, err := auth.ResolveAuthorizedServerID(db, r)
		if err != nil {
			writeAuthError(w, err)
			return
		}

		target := namespace + "/" + podName
		if containerName != "" {
			target += "/" + containerName
		}
		auditExec(db, r, "EXEC_POD", "k8s_pod", target, fmt.Sprintf("Interactive shell %v on server %d", cmd, serverID))
		runTerminal(w, r, func(ctx context.Context, streams docker.TerminalStreams) (int, error) {
			return service.ExecPodOnServer(ctx, serverID, namespace, podName, containerName, cmd, streams)
		})
	}
}
