package ecs

import (
	"context"
	"sync"
)

type World struct {
	entities map[EntityID]*Entity
	systems  []System
	events   chan *Event
	mu       sync.RWMutex
	ctx      context.Context
	cancel   context.CancelFunc
}

type Event struct {
	Type   string
	Data   interface{}
	Entity *Entity
}

func NewWorld() *World {
	ctx, cancel := context.WithCancel(context.Background())
	return &World{
		entities: make(map[EntityID]*Entity),
		systems:  make([]System, 0),
		events:   make(chan *Event, 1000),
		ctx:      ctx,
		cancel:   cancel,
	}
}

func (w *World) AddEntity(e *Entity) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.entities[e.ID] = e
}

func (w *World) RemoveEntity(id EntityID) {
	w.mu.Lock()
	defer w.mu.Unlock()
	delete(w.entities, id)
}

func (w *World) GetEntity(id EntityID) (*Entity, bool) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	e, ok := w.entities[id]
	return e, ok
}

func (w *World) Entities() []*Entity {
	w.mu.RLock()
	defer w.mu.RUnlock()
	result := make([]*Entity, 0, len(w.entities))
	for _, e := range w.entities {
		result = append(result, e)
	}
	return result
}

func (w *World) Query(q *EntityQuery) []*Entity {
	w.mu.RLock()
	defer w.mu.RUnlock()
	result := make([]*Entity, 0)
	for _, e := range w.entities {
		if q.Matches(e) {
			result = append(result, e)
		}
	}
	return result
}

func (w *World) AddSystem(s System) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.systems = append(w.systems, s)
}

func (w *World) RemoveSystem(name string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for i, s := range w.systems {
		if s.Name() == name {
			w.systems = append(w.systems[:i], w.systems[i+1:]...)
			return
		}
	}
}

func (w *World) Update(dt float64) {
	w.mu.RLock()
	systems := make([]System, len(w.systems))
	copy(systems, w.systems)
	w.mu.RUnlock()

	for _, s := range systems {
		s.Update(w, dt)
	}
}

func (w *World) EmitEvent(eventType string, data interface{}, entity *Entity) {
	select {
	case w.events <- &Event{
		Type:   eventType,
		Data:   data,
		Entity: entity,
	}:
	default:
	}
}

func (w *World) Events() <-chan *Event {
	return w.events
}

func (w *World) Close() {
	w.cancel()
	close(w.events)
}

func (w *World) Context() context.Context {
	return w.ctx
}
