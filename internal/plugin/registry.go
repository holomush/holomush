// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins

import (
	"sync"

	"github.com/samber/oops"
)

// ServiceRegistry maps proto service names to their registered implementations.
// Thread-safe for concurrent registration and resolution.
type ServiceRegistry struct {
	mu       sync.RWMutex
	services map[string]RegisteredService
}

// NewServiceRegistry creates an empty service registry.
func NewServiceRegistry() *ServiceRegistry {
	return &ServiceRegistry{services: make(map[string]RegisteredService)}
}

// Register adds a service to the registry. Returns an error if the service name is already registered.
func (r *ServiceRegistry) Register(svc RegisteredService) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.services[svc.Name]; exists {
		return oops.Code("SERVICE_ALREADY_REGISTERED").
			With("service", svc.Name).
			Errorf("service %q is already registered", svc.Name)
	}
	r.services[svc.Name] = svc
	return nil
}

// Resolve looks up a service by fully qualified proto name.
func (r *ServiceRegistry) Resolve(name string) (*RegisteredService, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	svc, ok := r.services[name]
	if !ok {
		return nil, oops.Code("SERVICE_NOT_FOUND").
			With("service", name).
			Errorf("service %q is not registered", name)
	}
	return &svc, nil
}

// Deregister removes a service from the registry.
func (r *ServiceRegistry) Deregister(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.services[name]; !ok {
		return oops.Code("SERVICE_NOT_FOUND").
			With("service", name).
			Errorf("service %q is not registered", name)
	}
	delete(r.services, name)
	return nil
}

// List returns all registered services.
func (r *ServiceRegistry) List() []RegisteredService {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]RegisteredService, 0, len(r.services))
	for _, svc := range r.services {
		result = append(result, svc)
	}
	return result
}
