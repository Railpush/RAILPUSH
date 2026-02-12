package handlers

import (
	"errors"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	"github.com/railpush/api/config"
	"github.com/railpush/api/middleware"
	"github.com/railpush/api/models"
	"github.com/railpush/api/services"
)

type WebSocketHandler struct {
	Config       *config.Config
	logClients   map[string]map[*websocket.Conn]bool
	buildClients map[string]map[*websocket.Conn]bool
	eventClients map[*websocket.Conn]bool
	mu           sync.RWMutex
}

func NewWebSocketHandler(cfg *config.Config) *WebSocketHandler {
	return &WebSocketHandler{
		Config:       cfg,
		logClients:   make(map[string]map[*websocket.Conn]bool),
		buildClients: make(map[string]map[*websocket.Conn]bool),
		eventClients: make(map[*websocket.Conn]bool),
	}
}

func normalizeHost(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if h, _, err := net.SplitHostPort(raw); err == nil {
		return strings.ToLower(strings.TrimSpace(h))
	}
	return strings.ToLower(strings.TrimSpace(raw))
}

func (h *WebSocketHandler) isAllowedOrigin(r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return false
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	originHost := normalizeHost(u.Host)
	if originHost == "" {
		return false
	}

	reqHost := normalizeHost(r.Host)
	if reqHost != "" && originHost == reqHost {
		return true
	}

	deployDomain := normalizeHost(h.Config.Deploy.Domain)
	if deployDomain != "" && (originHost == deployDomain || strings.HasSuffix(originHost, "."+deployDomain)) {
		return true
	}

	if originHost == "localhost" || originHost == "127.0.0.1" || originHost == "::1" {
		return true
	}
	return false
}

func (h *WebSocketHandler) upgrader() websocket.Upgrader {
	return websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			return h.isAllowedOrigin(r)
		},
	}
}

func (h *WebSocketHandler) authenticate(r *http.Request) (string, error) {
	userID, err := middleware.AuthenticateRequest(h.Config, r)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(userID) == "" {
		return "", errors.New("missing user id")
	}
	return userID, nil
}

func wsReject(w http.ResponseWriter, code int, message string) {
	http.Error(w, message, code)
}

func (h *WebSocketHandler) HandleLogStream(w http.ResponseWriter, r *http.Request) {
	userID, err := h.authenticate(r)
	if err != nil {
		wsReject(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	serviceID := strings.TrimSpace(mux.Vars(r)["serviceId"])
	if serviceID == "" {
		wsReject(w, http.StatusBadRequest, "missing service id")
		return
	}
	svc, err := models.GetService(serviceID)
	if err != nil || svc == nil {
		wsReject(w, http.StatusNotFound, "service not found")
		return
	}
	if err := services.EnsureWorkspaceAccess(userID, svc.WorkspaceID, models.RoleViewer); err != nil {
		wsReject(w, http.StatusForbidden, "forbidden")
		return
	}

	upgrader := h.upgrader()
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade failed: %v", err)
		return
	}
	h.mu.Lock()
	if h.logClients[serviceID] == nil {
		h.logClients[serviceID] = make(map[*websocket.Conn]bool)
	}
	h.logClients[serviceID][conn] = true
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		delete(h.logClients[serviceID], conn)
		h.mu.Unlock()
		conn.Close()
	}()
	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			break
		}
	}
}

func (h *WebSocketHandler) HandleBuildStream(w http.ResponseWriter, r *http.Request) {
	userID, err := h.authenticate(r)
	if err != nil {
		wsReject(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	deployID := strings.TrimSpace(mux.Vars(r)["deployId"])
	if deployID == "" {
		wsReject(w, http.StatusBadRequest, "missing deploy id")
		return
	}
	deploy, err := models.GetDeploy(deployID)
	if err != nil || deploy == nil {
		wsReject(w, http.StatusNotFound, "deploy not found")
		return
	}
	svc, err := models.GetService(deploy.ServiceID)
	if err != nil || svc == nil {
		wsReject(w, http.StatusNotFound, "service not found")
		return
	}
	if err := services.EnsureWorkspaceAccess(userID, svc.WorkspaceID, models.RoleViewer); err != nil {
		wsReject(w, http.StatusForbidden, "forbidden")
		return
	}

	upgrader := h.upgrader()
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade failed: %v", err)
		return
	}
	h.mu.Lock()
	if h.buildClients[deployID] == nil {
		h.buildClients[deployID] = make(map[*websocket.Conn]bool)
	}
	h.buildClients[deployID][conn] = true
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		delete(h.buildClients[deployID], conn)
		h.mu.Unlock()
		conn.Close()
	}()
	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			break
		}
	}
}

func (h *WebSocketHandler) HandleEventStream(w http.ResponseWriter, r *http.Request) {
	if _, err := h.authenticate(r); err != nil {
		wsReject(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	upgrader := h.upgrader()
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade failed: %v", err)
		return
	}
	h.mu.Lock()
	h.eventClients[conn] = true
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		delete(h.eventClients, conn)
		h.mu.Unlock()
		conn.Close()
	}()
	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			break
		}
	}
}

func (h *WebSocketHandler) BroadcastLogMessage(serviceID string, message []byte) {
	h.mu.RLock()
	var stale []*websocket.Conn
	for conn := range h.logClients[serviceID] {
		if err := conn.WriteMessage(websocket.TextMessage, message); err != nil {
			conn.Close()
			stale = append(stale, conn)
		}
	}
	h.mu.RUnlock()
	if len(stale) == 0 {
		return
	}
	h.mu.Lock()
	for _, conn := range stale {
		delete(h.logClients[serviceID], conn)
	}
	h.mu.Unlock()
}

func (h *WebSocketHandler) BroadcastBuildMessage(deployID string, message []byte) {
	h.mu.RLock()
	var stale []*websocket.Conn
	for conn := range h.buildClients[deployID] {
		if err := conn.WriteMessage(websocket.TextMessage, message); err != nil {
			conn.Close()
			stale = append(stale, conn)
		}
	}
	h.mu.RUnlock()
	if len(stale) == 0 {
		return
	}
	h.mu.Lock()
	for _, conn := range stale {
		delete(h.buildClients[deployID], conn)
	}
	h.mu.Unlock()
}

func (h *WebSocketHandler) BroadcastEvent(message []byte) {
	h.mu.RLock()
	var stale []*websocket.Conn
	for conn := range h.eventClients {
		if err := conn.WriteMessage(websocket.TextMessage, message); err != nil {
			conn.Close()
			stale = append(stale, conn)
		}
	}
	h.mu.RUnlock()
	if len(stale) == 0 {
		return
	}
	h.mu.Lock()
	for _, conn := range stale {
		delete(h.eventClients, conn)
	}
	h.mu.Unlock()
}
