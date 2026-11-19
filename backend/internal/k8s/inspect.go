package k8s

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	dockercontainer "github.com/docker/docker/api/types/container"
	"golang.org/x/crypto/ssh"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
	utilexec "k8s.io/client-go/util/exec"
	"sigs.k8s.io/yaml"

	"github.com/kraken/admin-tool/backend/internal/docker"
)

// EventSummary is a condensed Kubernetes event related to a described resource
type EventSummary struct {
	Type     string `json:"type"`
	Reason   string `json:"reason"`
	Object   string `json:"object"`
	Message  string `json:"message"`
	Count    int32  `json:"count"`
	LastSeen string `json:"last_seen"`
}

// ResourceDescription is the payload of a describe request (manifest + related events)
type ResourceDescription struct {
	Kind      string         `json:"kind"`
	Namespace string         `json:"namespace"`
	Name      string         `json:"name"`
	Source    string         `json:"source"` // "api" | "kubectl" | "docker"
	Manifest  string         `json:"manifest"`
	Describe  string         `json:"describe,omitempty"`
	Events    []EventSummary `json:"events"`
}

// describeKinds maps supported kinds to their kubectl resource names
var describeKinds = map[string]string{
	"pod":        "pod",
	"deployment": "deployment",
}

// eventBelongsTo matches events of the object itself and, for deployments, of its ReplicaSets and Pods
func eventBelongsTo(kind, name string, ev *corev1.Event) bool {
	objName := ev.InvolvedObject.Name
	if kind == "pod" {
		return strings.EqualFold(ev.InvolvedObject.Kind, "Pod") && objName == name
	}
	return objName == name || strings.HasPrefix(objName, name+"-")
}

func summarizeEvents(kind, name string, list *corev1.EventList) []EventSummary {
	type item struct {
		ts time.Time
		ev EventSummary
	}
	items := []item{}
	for i := range list.Items {
		ev := &list.Items[i]
		if !eventBelongsTo(kind, name, ev) {
			continue
		}
		ts := ev.LastTimestamp.Time
		if ts.IsZero() {
			ts = ev.EventTime.Time
		}
		if ts.IsZero() {
			ts = ev.CreationTimestamp.Time
		}
		count := ev.Count
		if count == 0 {
			count = 1
		}
		lastSeen := ""
		if !ts.IsZero() {
			lastSeen = ts.UTC().Format(time.RFC3339)
		}
		items = append(items, item{ts: ts, ev: EventSummary{
			Type:     ev.Type,
			Reason:   ev.Reason,
			Object:   strings.ToLower(ev.InvolvedObject.Kind) + "/" + ev.InvolvedObject.Name,
			Message:  strings.TrimSpace(ev.Message),
			Count:    count,
			LastSeen: lastSeen,
		}})
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].ts.Before(items[j].ts) })
	out := make([]EventSummary, 0, len(items))
	for _, it := range items {
		out = append(out, it.ev)
	}
	return out
}

// DescribeResourceOnServer returns the manifest and related events of a pod or deployment
func (s *K8sService) DescribeResourceOnServer(ctx context.Context, serverID uint, kind, namespace, name string) (*ResourceDescription, error) {
	kind = strings.ToLower(strings.TrimSpace(kind))
	resource, ok := describeKinds[kind]
	if !ok {
		return nil, fmt.Errorf("unsupported resource kind %q", kind)
	}
	if namespace == "" {
		namespace = "default"
	}
	if err := ValidateK8sNamespace(namespace); err != nil {
		return nil, err
	}
	if err := ValidateK8sName(name); err != nil {
		return nil, err
	}

	desc := &ResourceDescription{Kind: kind, Namespace: namespace, Name: name, Events: []EventSummary{}}

	useLocal, err := s.shouldUseLocalClient(serverID)
	if err != nil {
		return nil, err
	}
	if useLocal {
		if !s.IsConnected() {
			return nil, errors.New("kubernetes cluster is currently unavailable")
		}
		var obj interface{}
		switch kind {
		case "pod":
			pod, err := s.client.CoreV1().Pods(namespace).Get(ctx, name, metav1.GetOptions{})
			if err != nil {
				return nil, err
			}
			pod.ManagedFields = nil
			pod.APIVersion, pod.Kind = "v1", "Pod"
			obj = pod
		case "deployment":
			dep, err := s.client.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
			if err != nil {
				return nil, err
			}
			dep.ManagedFields = nil
			dep.APIVersion, dep.Kind = "apps/v1", "Deployment"
			obj = dep
		}
		manifest, err := yaml.Marshal(obj)
		if err != nil {
			return nil, err
		}
		desc.Source = "api"
		desc.Manifest = string(manifest)
		if events, err := s.client.CoreV1().Events(namespace).List(ctx, metav1.ListOptions{}); err == nil {
			desc.Events = summarizeEvents(kind, name, events)
		}
		return desc, nil
	}

	// 1. Remote kubectl over SSH
	cli, cliErr := s.detectRemoteK8sCLI(ctx, serverID)
	if cliErr == nil && cli != "" {
		target := fmt.Sprintf("%s %s -n %s", resource, shellEscape(name), shellEscape(namespace))
		manifest, err := s.execRemoteSSH(ctx, serverID, fmt.Sprintf("%s get %s -o yaml", cli, target))
		if err == nil {
			desc.Source = "kubectl"
			desc.Manifest = manifest
			if out, err := s.execRemoteSSH(ctx, serverID, fmt.Sprintf("%s describe %s", cli, target)); err == nil {
				desc.Describe = out
			}
			if out, err := s.execRemoteSSH(ctx, serverID, fmt.Sprintf("%s get events -n %s -o json", cli, shellEscape(namespace))); err == nil {
				var events corev1.EventList
				if json.Unmarshal([]byte(out), &events) == nil {
					desc.Events = summarizeEvents(kind, name, &events)
				}
			}
			return desc, nil
		}
		if kind != "pod" {
			return nil, err
		}
		log.Printf("[K8S] kubectl describe failed on server %d: %v. Falling back to Docker inspection.", serverID, err)
	} else if kind != "pod" {
		return nil, fmt.Errorf("kubernetes CLI not available on remote server: %w", cliErr)
	}

	// 2. Docker fallback (pods only): inspect the containers that back the pod
	if s.docker == nil {
		return nil, errors.New("docker service not available")
	}
	ids, err := s.podContainerIDsViaDocker(ctx, serverID, namespace, name, "")
	if err != nil {
		return nil, err
	}
	inspects := make([]json.RawMessage, 0, len(ids))
	for _, id := range ids {
		if raw, err := s.docker.InspectContainerOnServer(ctx, serverID, id); err == nil {
			inspects = append(inspects, raw)
		}
	}
	rawList, _ := json.Marshal(inspects)
	manifest, err := yaml.JSONToYAML(rawList)
	if err != nil {
		return nil, err
	}
	desc.Source = "docker"
	desc.Manifest = string(manifest)
	return desc, nil
}

// podContainerIDsViaDocker finds docker containers backing a pod (optionally a single container of it)
func (s *K8sService) podContainerIDsViaDocker(ctx context.Context, serverID uint, namespace, podName, containerName string) ([]string, error) {
	cli, err := s.docker.GetClient(ctx, serverID)
	if err != nil || cli == nil {
		return nil, errors.New("docker client unreachable")
	}
	rawList, err := cli.ContainerList(ctx, dockercontainer.ListOptions{All: true})
	if err != nil {
		return nil, err
	}
	ids := []string{}
	for _, c := range rawList {
		pNs := c.Labels["io.kubernetes.pod.namespace"]
		if pNs == "" {
			pNs = "default"
		}
		cName := c.Labels["io.kubernetes.container.name"]
		if c.Labels["io.kubernetes.pod.name"] != podName || pNs != namespace || cName == "POD" {
			continue
		}
		if containerName != "" && cName != containerName {
			continue
		}
		ids = append(ids, c.ID)
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("container for pod %s/%s not found on server %d", namespace, podName, serverID)
	}
	return ids, nil
}

// terminalSizeQueue adapts a TerminalSize channel to remotecommand.TerminalSizeQueue
type terminalSizeQueue struct {
	ctx context.Context
	ch  <-chan docker.TerminalSize
}

func (q *terminalSizeQueue) Next() *remotecommand.TerminalSize {
	select {
	case <-q.ctx.Done():
		return nil
	case size, ok := <-q.ch:
		if !ok {
			return nil
		}
		return &remotecommand.TerminalSize{Width: size.Cols, Height: size.Rows}
	}
}

// ExecPodOnServer opens an interactive TTY shell in a pod container and blocks until it exits.
// It returns the remote process exit code.
func (s *K8sService) ExecPodOnServer(ctx context.Context, serverID uint, namespace, podName, containerName string, cmd []string, streams docker.TerminalStreams) (int, error) {
	if namespace == "" {
		namespace = "default"
	}
	if err := ValidateK8sNamespace(namespace); err != nil {
		return -1, err
	}
	if err := ValidateK8sName(podName); err != nil {
		return -1, err
	}
	if err := ValidateContainerName(containerName); err != nil {
		return -1, err
	}

	useLocal, err := s.shouldUseLocalClient(serverID)
	if err != nil {
		return -1, err
	}
	if useLocal {
		if !s.IsConnected() || s.restCfg == nil {
			return -1, errors.New("kubernetes cluster is currently unavailable")
		}
		return s.execPodViaAPI(ctx, namespace, podName, containerName, cmd, streams)
	}

	cli, cliErr := s.detectRemoteK8sCLI(ctx, serverID)
	if cliErr == nil && cli != "" {
		return s.execPodViaSSH(ctx, serverID, cli, namespace, podName, containerName, cmd, streams)
	}

	if s.docker == nil {
		return -1, fmt.Errorf("kubernetes CLI not available on remote server: %w", cliErr)
	}
	ids, err := s.podContainerIDsViaDocker(ctx, serverID, namespace, podName, containerName)
	if err != nil {
		return -1, err
	}
	return s.docker.ExecTTYOnServer(ctx, serverID, ids[0], cmd, streams)
}

func (s *K8sService) execPodViaAPI(ctx context.Context, namespace, podName, containerName string, cmd []string, streams docker.TerminalStreams) (int, error) {
	cfg := rest.CopyConfig(s.restCfg)
	cfg.Timeout = 0 // interactive sessions are long-lived

	req := s.client.CoreV1().RESTClient().Post().
		Resource("pods").
		Namespace(namespace).
		Name(podName).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: containerName,
			Command:   cmd,
			Stdin:     true,
			Stdout:    true,
			Stderr:    true,
			TTY:       true,
		}, scheme.ParameterCodec)

	executor, err := remotecommand.NewSPDYExecutor(cfg, "POST", req.URL())
	if err != nil {
		return -1, err
	}
	err = executor.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdin:             streams.Stdin,
		Stdout:            streams.Stdout,
		Tty:               true,
		TerminalSizeQueue: &terminalSizeQueue{ctx: ctx, ch: streams.Resize},
	})
	if err != nil {
		var exitErr utilexec.ExitError
		if errors.As(err, &exitErr) {
			return exitErr.ExitStatus(), nil
		}
		return -1, err
	}
	return 0, nil
}

func (s *K8sService) execPodViaSSH(ctx context.Context, serverID uint, cli, namespace, podName, containerName string, cmd []string, streams docker.TerminalStreams) (int, error) {
	sshClient, server, err := s.docker.GetSSHClient(ctx, serverID)
	if err != nil {
		return -1, err
	}
	defer sshClient.Close()

	session, err := sshClient.NewSession()
	if err != nil {
		return -1, fmt.Errorf("failed to create SSH session on %s: %w", server.Name, err)
	}
	defer session.Close()

	size := docker.TerminalSize{Cols: 80, Rows: 24}
	select {
	case first, ok := <-streams.Resize:
		if ok {
			size = first
		}
	case <-time.After(200 * time.Millisecond):
	}

	modes := ssh.TerminalModes{ssh.ECHO: 1, ssh.TTY_OP_ISPEED: 14400, ssh.TTY_OP_OSPEED: 14400}
	if err := session.RequestPty("xterm-256color", int(size.Rows), int(size.Cols), modes); err != nil {
		return -1, fmt.Errorf("failed to allocate remote pty: %w", err)
	}
	session.Stdin = streams.Stdin
	session.Stdout = streams.Stdout
	session.Stderr = streams.Stdout

	quoted := make([]string, len(cmd))
	for i, part := range cmd {
		quoted[i] = shellEscape(part)
	}
	kubectlCmd := fmt.Sprintf("%s exec -it %s -n %s", cli, shellEscape(podName), shellEscape(namespace))
	if containerName != "" {
		kubectlCmd += " -c " + shellEscape(containerName)
	}
	kubectlCmd += " -- " + strings.Join(quoted, " ")

	// Wrap in POSIX sh so the remote login shell (fish, zsh, ...) does not reinterpret the command
	if err := session.Start("sh -c " + shellEscape(kubectlCmd)); err != nil {
		return -1, fmt.Errorf("failed to start kubectl exec on %s: %w", server.Name, err)
	}

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case sz, ok := <-streams.Resize:
				if !ok {
					return
				}
				_ = session.WindowChange(int(sz.Rows), int(sz.Cols))
			}
		}
	}()

	done := make(chan error, 1)
	go func() { done <- session.Wait() }()

	select {
	case <-ctx.Done():
		_ = session.Signal(ssh.SIGHUP)
		return -1, ctx.Err()
	case err := <-done:
		if err == nil {
			return 0, nil
		}
		var exitErr *ssh.ExitError
		if errors.As(err, &exitErr) {
			return exitErr.ExitStatus(), nil
		}
		return -1, err
	}
}
