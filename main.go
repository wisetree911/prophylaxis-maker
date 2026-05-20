package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

const (
	defaultAddr           = ":8080"
	defaultDataFile       = "data/maintenances.yaml"
	defaultSSHTimeout     = 10 * time.Second
	defaultCommandTimeout = 2 * time.Minute
)

type App struct {
	store          *Store
	sshTimeout     time.Duration
	commandTimeout time.Duration
}

type runResult struct {
	ID     string `json:"id"`
	Name   string `json:"name,omitempty"`
	Output string `json:"output"`
	Error  string `json:"error,omitempty"`
}

func main() {
	addr := env("ADDR", defaultAddr)
	dataFile := env("DATA_FILE", defaultDataFile)

	store, err := NewStore(dataFile)
	if err != nil {
		log.Fatalf("load store: %v", err)
	}

	app := &App{
		store:          store,
		sshTimeout:     defaultSSHTimeout,
		commandTimeout: durationEnv("COMMAND_TIMEOUT", defaultCommandTimeout),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", app.handleHealthz)
	mux.HandleFunc("/maintenances", app.handleMaintenances)
	mux.HandleFunc("/maintenances/", app.handleMaintenancePath)

	log.Printf("listening on %s, data file %s", addr, dataFile)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal(err)
	}
}

func (a *App) handleHealthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (a *App) handleMaintenances(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, a.store.List())
	case http.MethodPost:
		var m Maintenance
		if err := json.NewDecoder(r.Body).Decode(&m); err != nil {
			writeError(w, http.StatusBadRequest, "invalid json")
			return
		}
		m.ID = newID()
		normalizePorts(&m)
		if err := validateMaintenance(m); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		created, err := a.store.Add(m)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, created)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (a *App) handleMaintenancePath(w http.ResponseWriter, r *http.Request) {
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/maintenances/"), "/")
	if path == "run" {
		a.handleRun(w, r)
		return
	}

	parts := strings.Split(path, "/")
	if len(parts) != 2 || r.Method != http.MethodPost {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	id, action := parts[0], parts[1]
	switch action {
	case "activate":
		a.setActive(w, id, true)
	case "deactivate":
		a.setActive(w, id, false)
	default:
		writeError(w, http.StatusNotFound, "not found")
	}
}

func (a *App) setActive(w http.ResponseWriter, id string, active bool) {
	updated, err := a.store.SetActive(id, active)
	if errors.Is(err, os.ErrNotExist) {
		writeError(w, http.StatusNotFound, "maintenance not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (a *App) handleRun(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var active []Maintenance
	for _, m := range a.store.List() {
		if m.Active {
			active = append(active, m)
		}
	}

	results := make([]runResult, len(active))
	var wg sync.WaitGroup
	for i, m := range active {
		wg.Add(1)
		go func(i int, m Maintenance) {
			defer wg.Done()

			if err := validateMaintenance(m); err != nil {
				results[i] = runResult{
					ID:    m.ID,
					Name:  m.Name,
					Error: err.Error(),
				}
				return
			}

			ctx, cancel := context.WithTimeout(r.Context(), a.commandTimeout)
			defer cancel()

			output, err := runRemoteCommand(ctx, m, a.sshTimeout)
			results[i] = runResult{
				ID:     m.ID,
				Name:   m.Name,
				Output: output,
			}
			if err != nil {
				results[i].Error = err.Error()
			}
		}(i, m)
	}
	wg.Wait()

	writeJSON(w, http.StatusOK, map[string]any{
		"count":   len(results),
		"results": results,
	})
}

func runRemoteCommand(ctx context.Context, m Maintenance, timeout time.Duration) (string, error) {
	bastionConfig, err := sshClientConfig(m.Bastion, timeout)
	if err != nil {
		return "", fmt.Errorf("bastion auth: %w", err)
	}
	targetConfig, err := sshClientConfig(m.Host, timeout)
	if err != nil {
		return "", fmt.Errorf("host auth: %w", err)
	}

	bastionAddr := sshAddress(m.Bastion)
	bastionClient, err := ssh.Dial("tcp", bastionAddr, bastionConfig)
	if err != nil {
		return "", fmt.Errorf("connect bastion %s: %w", bastionAddr, err)
	}
	defer bastionClient.Close()

	targetAddr := sshAddress(m.Host)
	targetConn, err := bastionClient.Dial("tcp", targetAddr)
	if err != nil {
		return "", fmt.Errorf("connect host %s through bastion: %w", targetAddr, err)
	}

	targetSSHConn, chans, reqs, err := ssh.NewClientConn(targetConn, targetAddr, targetConfig)
	if err != nil {
		return "", fmt.Errorf("ssh host %s: %w", targetAddr, err)
	}
	targetClient := ssh.NewClient(targetSSHConn, chans, reqs)
	defer targetClient.Close()

	session, err := targetClient.NewSession()
	if err != nil {
		return "", fmt.Errorf("open session: %w", err)
	}
	defer session.Close()

	done := make(chan commandResult, 1)
	go func() {
		out, err := session.CombinedOutput(m.Command)
		done <- commandResult{output: string(out), err: err}
	}()

	select {
	case res := <-done:
		return res.output, res.err
	case <-ctx.Done():
		_ = session.Close()
		return "", ctx.Err()
	}
}

type commandResult struct {
	output string
	err    error
}

func sshClientConfig(h SSHHost, timeout time.Duration) (*ssh.ClientConfig, error) {
	methods, err := authMethods(h.Auth)
	if err != nil {
		return nil, err
	}
	return &ssh.ClientConfig{
		User:            h.User,
		Auth:            methods,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         timeout,
	}, nil
}

func authMethods(auth SSHAuth) ([]ssh.AuthMethod, error) {
	var methods []ssh.AuthMethod

	if auth.Password != "" {
		methods = append(methods, ssh.Password(auth.Password))
	}
	if auth.PrivateKeyPath != "" {
		key, err := os.ReadFile(auth.PrivateKeyPath)
		if err != nil {
			return nil, fmt.Errorf("read private key: %w", err)
		}

		var signer ssh.Signer
		if auth.PrivateKeyPassphrase != "" {
			signer, err = ssh.ParsePrivateKeyWithPassphrase(key, []byte(auth.PrivateKeyPassphrase))
		} else {
			signer, err = ssh.ParsePrivateKey(key)
		}
		if err != nil {
			return nil, fmt.Errorf("parse private key: %w", err)
		}
		methods = append(methods, ssh.PublicKeys(signer))
	}
	if len(methods) == 0 {
		return nil, errors.New("password or private_key_path is required")
	}
	return methods, nil
}

func validateMaintenance(m Maintenance) error {
	if strings.TrimSpace(m.Command) == "" {
		return errors.New("command is required")
	}
	if err := validateHost("host", m.Host); err != nil {
		return err
	}
	if err := validateHost("bastion", m.Bastion); err != nil {
		return err
	}
	return nil
}

func validateHost(name string, h SSHHost) error {
	if strings.TrimSpace(h.Address) == "" {
		return fmt.Errorf("%s.address is required", name)
	}
	if strings.TrimSpace(h.User) == "" {
		return fmt.Errorf("%s.user is required", name)
	}
	if h.Port < 0 || h.Port > 65535 {
		return fmt.Errorf("%s.port is invalid", name)
	}
	if h.Auth.Password == "" && h.Auth.PrivateKeyPath == "" {
		return fmt.Errorf("%s.auth.password or %s.auth.private_key_path is required", name, name)
	}
	return nil
}

func normalizePorts(m *Maintenance) {
	if m.Host.Port == 0 {
		m.Host.Port = 22
	}
	if m.Bastion.Port == 0 {
		m.Bastion.Port = 22
	}
}

func sshAddress(h SSHHost) string {
	return net.JoinHostPort(h.Address, strconv.Itoa(h.Port))
}

func newID() string {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return hex.EncodeToString(b)
}

func env(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func durationEnv(name string, fallback time.Duration) time.Duration {
	value := os.Getenv(name)
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("write response: %v", err)
	}
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
