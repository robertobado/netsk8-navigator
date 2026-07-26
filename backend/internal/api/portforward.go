package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"
)

// pfSession tracks one active port-forward tunnel, keyed by an opaque id in
// Server.pf. The local listener always binds 127.0.0.1 only — same model as
// `kubectl port-forward`: the client connecting to localPort is expected to
// run on this machine, not on whatever device is remotely viewing the UI.
type pfSession struct {
	namespace string
	pod       string
	port      int32
	localPort int
	stopCh    chan struct{}
}

// handleStartPortForward opens a tunnel from a local, loopback-only port to a
// pod's port. POST /api/contexts/{ctx}/portforward/{namespace}/{name}, body
// {"port": N}. Responds with the id (to stop it later) and the assigned
// local port.
func (s *Server) handleStartPortForward(w http.ResponseWriter, r *http.Request) {
	if s.demoModeBlocked(w) {
		return
	}
	namespace := r.PathValue("namespace")
	name := r.PathValue("name")

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<10))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	var payload struct {
		Port int32 `json:"port"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if payload.Port < 1 || payload.Port > 65535 {
		writeError(w, http.StatusBadRequest, fmt.Errorf("port must be between 1 and 65535"))
		return
	}

	client, err := s.mgr.ClientFor(r.PathValue("ctx"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	restCfg, err := s.mgr.RESTConfigFor(r.PathValue("ctx"))
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}

	req := client.CoreV1().RESTClient().Post().Resource("pods").Namespace(namespace).Name(name).SubResource("portforward")
	roundTripper, upgrader, err := spdy.RoundTripperFor(restCfg)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: roundTripper}, "POST", req.URL())

	stopCh := make(chan struct{})
	readyCh := make(chan struct{})
	pfwd, err := portforward.NewOnAddresses(dialer, []string{"127.0.0.1"}, []string{fmt.Sprintf("0:%d", payload.Port)}, stopCh, readyCh, io.Discard, io.Discard)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	errCh := make(chan error, 1)
	go func() { errCh <- pfwd.ForwardPorts() }()

	select {
	case <-readyCh:
	case err := <-errCh:
		writeError(w, http.StatusBadGateway, fmt.Errorf("port-forward failed: %w", err))
		return
	case <-time.After(10 * time.Second):
		close(stopCh)
		writeError(w, http.StatusGatewayTimeout, fmt.Errorf("timed out waiting for the tunnel to become ready"))
		return
	}

	ports, err := pfwd.GetPorts()
	if err != nil || len(ports) == 0 {
		close(stopCh)
		writeError(w, http.StatusInternalServerError, fmt.Errorf("could not determine the assigned local port"))
		return
	}

	id := fmt.Sprintf("%s-%s-%d-%d", namespace, name, payload.Port, time.Now().UnixNano())
	sess := &pfSession{namespace: namespace, pod: name, port: payload.Port, localPort: int(ports[0].Local), stopCh: stopCh}

	s.pfMu.Lock()
	s.pf[id] = sess
	s.pfMu.Unlock()
	audit(r, "port-forward-start", "namespace", namespace, "pod", name, "port", fmt.Sprintf("%d", payload.Port))

	// Self-clean once the tunnel ends, whether from handleStopPortForward
	// closing stopCh or the connection dropping on its own (pod deleted, etc).
	go func() {
		<-errCh
		s.pfMu.Lock()
		delete(s.pf, id)
		s.pfMu.Unlock()
	}()

	writeJSON(w, http.StatusOK, map[string]any{"id": id, "localPort": sess.localPort})
}

// handleStopPortForward closes an active tunnel. DELETE /api/contexts/{ctx}/portforward/{id}
func (s *Server) handleStopPortForward(w http.ResponseWriter, r *http.Request) {
	if s.demoModeBlocked(w) {
		return
	}
	id := r.PathValue("id")
	s.pfMu.Lock()
	sess, ok := s.pf[id]
	if ok {
		delete(s.pf, id)
	}
	s.pfMu.Unlock()
	if !ok {
		writeError(w, http.StatusNotFound, fmt.Errorf("no active port-forward with id %q", id))
		return
	}
	close(sess.stopCh)
	audit(r, "port-forward-stop", "namespace", sess.namespace, "pod", sess.pod, "port", fmt.Sprintf("%d", sess.port))
	writeJSON(w, http.StatusOK, map[string]string{"status": "stopped"})
}

type portForwardView struct {
	ID        string `json:"id"`
	Namespace string `json:"namespace"`
	Pod       string `json:"pod"`
	Port      int32  `json:"port"`
	LocalPort int    `json:"localPort"`
}

// handleListPortForwards lists every tunnel currently open on this server
// process (across all contexts — there's only ever one navigator instance
// per machine). GET /api/contexts/{ctx}/portforward
func (s *Server) handleListPortForwards(w http.ResponseWriter, r *http.Request) {
	if s.demoModeBlocked(w) {
		return
	}
	s.pfMu.Lock()
	defer s.pfMu.Unlock()
	out := make([]portForwardView, 0, len(s.pf))
	for id, sess := range s.pf {
		out = append(out, portForwardView{ID: id, Namespace: sess.namespace, Pod: sess.pod, Port: sess.port, LocalPort: sess.localPort})
	}
	writeJSON(w, http.StatusOK, out)
}
