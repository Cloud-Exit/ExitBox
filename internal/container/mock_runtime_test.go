// ExitBox - Multi-Agent Container Sandbox
// Copyright (C) 2026 Cloud Exit B.V.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.

package container

import (
	"context"
	"sync"
	"testing"
)

// MockRuntime implements Runtime for testing.
type MockRuntime struct {
	mu         sync.Mutex
	NameVal    string
	Images     map[string]map[string]string // image -> labels
	Containers []string
	Networks   map[string]bool
	Removed    []string
	Built      [][]string
}

func NewMockRuntime() *MockRuntime {
	return &MockRuntime{
		NameVal:  "docker",
		Images:   make(map[string]map[string]string),
		Networks: make(map[string]bool),
	}
}

func (m *MockRuntime) Name() string { return m.NameVal }

func (m *MockRuntime) Build(_ context.Context, args []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Built = append(m.Built, args)
	return nil
}

func (m *MockRuntime) Run(_ context.Context, _ []string) (int, error) { return 0, nil }
func (m *MockRuntime) Exec(_ context.Context, _ string, _ []string) error { return nil }

func (m *MockRuntime) ImageExists(image string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.Images[image]
	return ok
}

func (m *MockRuntime) ImageInspect(image, format string) (string, error) { return "", nil }

func (m *MockRuntime) ImageList(filter string) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var result []string
	for name := range m.Images {
		result = append(result, name)
	}
	return result, nil
}

func (m *MockRuntime) ImageRemove(image string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Removed = append(m.Removed, image)
	delete(m.Images, image)
	return nil
}

func (m *MockRuntime) PS(filter, format string) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.Containers, nil
}

func (m *MockRuntime) Stop(_ string) error  { return nil }
func (m *MockRuntime) Remove(_ string) error { return nil }

func (m *MockRuntime) NetworkCreate(name string, _ bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Networks[name] = true
	return nil
}

func (m *MockRuntime) NetworkExists(name string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.Networks[name]
}

func (m *MockRuntime) NetworkConnect(_, _ string) error           { return nil }
func (m *MockRuntime) NetworkInspect(_, _ string) (string, error) { return "", nil }
func (m *MockRuntime) IsRootless() bool                           { return false }

func TestMockRuntimeImplementsInterface(t *testing.T) {
	var _ Runtime = NewMockRuntime()
}
