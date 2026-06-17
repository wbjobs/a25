package ecs

type System interface {
	Update(world *World, dt float64)
	Name() string
}

type BaseSystem struct{}

func (BaseSystem) Name() string { return "base_system" }

type EntityQuery struct {
	RequiredComponents []string
}

func (q *EntityQuery) Matches(e *Entity) bool {
	for _, comp := range q.RequiredComponents {
		if !e.HasComponent(comp) {
			return false
		}
	}
	return true
}
