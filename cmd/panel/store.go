package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Progress хранится в .progress.json в корне репозитория.
type Progress struct {
	Tasks  map[string]TaskProgress `json:"tasks"`
	Checks map[string]time.Time    `json:"checks"` // id → когда отмечено
}

type TaskProgress struct {
	Passed     bool      `json:"passed"`
	PassedOnce bool      `json:"passedOnce"` // хоть раз проходила — чтобы регресс не стирал историю
	Ok         int       `json:"ok"`
	Total      int       `json:"total"`
	LastRun    time.Time `json:"lastRun"`
}

type Store struct {
	path string
	mu   sync.Mutex
	p    Progress
}

func openStore(path string) (*Store, error) {
	s := &Store{path: path, p: Progress{Tasks: map[string]TaskProgress{}, Checks: map[string]time.Time{}}}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, &s.p); err != nil {
		return nil, err
	}
	if s.p.Tasks == nil {
		s.p.Tasks = map[string]TaskProgress{}
	}
	if s.p.Checks == nil {
		s.p.Checks = map[string]time.Time{}
	}
	return s, nil
}

func (s *Store) Snapshot() Progress {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Сериализация в JSON дальше идёт без мьютекса, поэтому отдаём копии мап.
	out := Progress{Tasks: make(map[string]TaskProgress, len(s.p.Tasks)), Checks: make(map[string]time.Time, len(s.p.Checks))}
	for k, v := range s.p.Tasks {
		out.Tasks[k] = v
	}
	for k, v := range s.p.Checks {
		out.Checks[k] = v
	}
	return out
}

func (s *Store) SetTask(path string, tp TaskProgress) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tp.PassedOnce = tp.Passed || s.p.Tasks[path].PassedOnce
	s.p.Tasks[path] = tp
	return s.save()
}

func (s *Store) SetCheck(id string, done bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if done {
		s.p.Checks[id] = time.Now()
	} else {
		delete(s.p.Checks, id)
	}
	return s.save()
}

// save пишет атомарно: временный файл + rename. Вызывать под мьютексом.
func (s *Store) save() error {
	b, err := json.MarshalIndent(s.p, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".progress-*.json")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), s.path)
}
