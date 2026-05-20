package main

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

type DataFile struct {
	Maintenances []Maintenance `json:"maintenances" yaml:"maintenances"`
}

type Store struct {
	path string
	mu   sync.Mutex
	data DataFile
}

func NewStore(path string) (*Store, error) {
	s := &Store{path: path}
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("data file %q does not exist", path)
	}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	raw, err := os.ReadFile(s.path)
	if err != nil {
		return err
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		s.data = DataFile{}
		return nil
	}
	if err := yaml.Unmarshal(raw, &s.data); err != nil {
		return err
	}
	for i := range s.data.Maintenances {
		normalizePorts(&s.data.Maintenances[i])
	}
	return nil
}

func (s *Store) saveLocked() error {
	raw, err := yaml.Marshal(s.data)
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, raw, 0o600)
}

func (s *Store) Add(m Maintenance) (Maintenance, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, existing := range s.data.Maintenances {
		if existing.ID == m.ID {
			return Maintenance{}, fmt.Errorf("maintenance with id %q already exists", m.ID)
		}
	}

	s.data.Maintenances = append(s.data.Maintenances, m)
	return m, s.saveLocked()
}

func (s *Store) List() []Maintenance {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]Maintenance(nil), s.data.Maintenances...)
}

func (s *Store) SetActive(id string, active bool) (Maintenance, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i := range s.data.Maintenances {
		if s.data.Maintenances[i].ID == id {
			s.data.Maintenances[i].Active = active
			return s.data.Maintenances[i], s.saveLocked()
		}
	}
	return Maintenance{}, os.ErrNotExist
}
