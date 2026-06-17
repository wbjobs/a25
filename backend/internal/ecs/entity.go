package ecs

import (
	"sync"
	"sync/atomic"
)

type EntityID uint64

type Entity struct {
	ID         EntityID
	components map[string]Component
	mu         sync.RWMutex
}

var entityCounter uint64

func NewEntity() *Entity {
	id := atomic.AddUint64(&entityCounter, 1)
	return &Entity{
		ID:         EntityID(id),
		components: make(map[string]Component),
	}
}

func (e *Entity) AddComponent(c Component) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.components[c.Type()] = c
}

func (e *Entity) RemoveComponent(componentType string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.components, componentType)
}

func (e *Entity) GetComponent(componentType string) (Component, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	c, ok := e.components[componentType]
	return c, ok
}

func (e *Entity) HasComponent(componentType string) bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	_, ok := e.components[componentType]
	return ok
}

func (e *Entity) Components() map[string]Component {
	e.mu.RLock()
	defer e.mu.RUnlock()
	result := make(map[string]Component, len(e.components))
	for k, v := range e.components {
		result[k] = v
	}
	return result
}
