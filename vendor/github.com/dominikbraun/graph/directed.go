package graph

import (
	"errors"
	"fmt"
)

type directed[K comparable, T any] struct {
	hash   Hash[K, T]
	traits *Traits
	store  Store[K, T]
}

func newDirected[K comparable, T any](hash Hash[K, T], traits *Traits, store Store[K, T]) *directed[K, T] {
	return &directed[K, T]{
		hash:   hash,
		traits: traits,
		store:  store,
	}
}

func (d *directed[K, T]) Traits() *Traits {
	return d.traits
}

func (d *directed[K, T]) AddVertex(value T, options ...func(*VertexProperties)) error {
	hash := d.hash(value)
	properties := VertexProperties{
		Weight:     0,
		Attributes: make(map[string]string),
	}

	for _, option := range options {
		option(&properties)
	}

	return d.store.AddVertex(hash, value, properties)
}

func (d *directed[K, T]) Vertex(hash K) (T, error) {
	vertex, _, err := d.store.Vertex(hash)
	return vertex, err
}

func (d *directed[K, T]) VertexWithProperties(hash K) (T, VertexProperties, error) {
	vertex, properties, err := d.store.Vertex(hash)
	if err != nil {
		return vertex, VertexProperties{}, err
	}

	return vertex, properties, nil
}

func (d *directed[K, T]) AddEdge(sourceHash, targetHash K, options ...func(*EdgeProperties)) error {
	_, _, err := d.store.Vertex(sourceHash)
	if err != nil {
		return fmt.Errorf("source vertex %v: %w", sourceHash, err)
	}

	_, _, err = d.store.Vertex(targetHash)
	if err != nil {
		return fmt.Errorf("target vertex %v: %w", targetHash, err)
	}

	if _, err := d.Edge(sourceHash, targetHash); !errors.Is(err, ErrEdgeNotFound) {
		return ErrEdgeAlreadyExists
	}

	// If the user opted in to preventing cycles, run a cycle check.
	if d.traits.PreventCycles {
		createsCycle, err := CreatesCycle[K, T](d, sourceHash, targetHash)
		if err != nil {
			return fmt.Errorf("check for cycles: %w", err)
		}
		if createsCycle {
			return ErrEdgeCreatesCycle
		}
	}

	edge := Edge[K]{
		Source: sourceHash,
		Target: targetHash,
		Properties: EdgeProperties{
			Attributes: make(map[string]string),
		},
	}

	for _, option := range options {
		option(&edge.Properties)
	}

	return d.addEdge(sourceHash, targetHash, edge)
}

func (d *directed[K, T]) Edge(sourceHash, targetHash K) (Edge[T], error) {
	edge, err := d.store.Edge(sourceHash, targetHash)
	if err != nil {
		return Edge[T]{}, err
	}

	sourceVertex, _, err := d.store.Vertex(sourceHash)
	if err != nil {
		return Edge[T]{}, err
	}

	targetVertex, _, err := d.store.Vertex(targetHash)
	if err != nil {
		return Edge[T]{}, err
	}

	return Edge[T]{
		Source: sourceVertex,
		Target: targetVertex,
		Properties: EdgeProperties{
			Weight:     edge.Properties.Weight,
			Attributes: edge.Properties.Attributes,
			Data:       edge.Properties.Data,
		},
	}, nil
}

func (d *directed[K, T]) RemoveEdge(source, target K) error {
	if _, err := d.Edge(source, target); err != nil {
		return err
	}

	if err := d.store.RemoveEdge(source, target); err != nil {
		return fmt.Errorf("failed to remove edge from %v to %v: %w", source, target, err)
	}

	return nil
}

func (d *directed[K, T]) AdjacencyMap() (map[K]map[K]Edge[K], error) {
	vertices, err := d.store.ListVertices()
	if err != nil {
		return nil, fmt.Errorf("failed to list vertices: %w", err)
	}

	edges, err := d.store.ListEdges()
	if err != nil {
		return nil, fmt.Errorf("failed to list edges: %w", err)
	}

	m := make(map[K]map[K]Edge[K])

	for _, vertex := range vertices {
		m[vertex] = make(map[K]Edge[K])
	}

	for _, edge := range edges {
		m[edge.Source][edge.Target] = edge
	}

	return m, nil
}

func (d *directed[K, T]) PredecessorMap() (map[K]map[K]Edge[K], error) {
	m := make(map[K]map[K]Edge[K])

	vertices, err := d.store.ListVertices()
	if err != nil {
		return nil, fmt.Errorf("failed to list vertices: %w", err)
	}

	edges, err := d.store.ListEdges()
	if err != nil {
		return nil, fmt.Errorf("failed to list edges: %w", err)
	}

	for _, vertex := range vertices {
		m[vertex] = make(map[K]Edge[K])
	}

	for _, edge := range edges {
		if _, ok := m[edge.Target]; !ok {
			m[edge.Target] = make(map[K]Edge[K])
		}
		m[edge.Target][edge.Source] = edge
	}

	return m, nil
}

func (d *directed[K, T]) addEdge(sourceHash, targetHash K, edge Edge[K]) error {
	return d.store.AddEdge(sourceHash, targetHash, edge)
}

func (d *directed[K, T]) Clone() (Graph[K, T], error) {
	traits := &Traits{
		IsDirected: d.traits.IsDirected,
		IsAcyclic:  d.traits.IsAcyclic,
		IsWeighted: d.traits.IsWeighted,
		IsRooted:   d.traits.IsRooted,
	}

	return &directed[K, T]{
		hash:   d.hash,
		traits: traits,
		store:  d.store,
	}, nil
}

func (d *directed[K, T]) Order() (int, error) {
	return d.store.VertexCount()
}

func (d *directed[K, T]) Size() (int, error) {
	size := 0
	outEdges, err := d.AdjacencyMap()
	if err != nil {
		return 0, fmt.Errorf("failed to get adjacency map: %w", err)
	}

	for _, outEdges := range outEdges {
		size += len(outEdges)
	}

	return size, nil
}

func (d *directed[K, T]) edgesAreEqual(a, b Edge[T]) bool {
	aSourceHash := d.hash(a.Source)
	aTargetHash := d.hash(a.Target)
	bSourceHash := d.hash(b.Source)
	bTargetHash := d.hash(b.Target)

	return aSourceHash == bSourceHash && aTargetHash == bTargetHash
}
