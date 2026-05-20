package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"
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
