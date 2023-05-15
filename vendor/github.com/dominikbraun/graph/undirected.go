package graph

import (
	"errors"
	"fmt"
)

type undirected[K comparable, T any] struct {
	hash   Hash[K, T]
	traits *Traits
	store  Store[K, T]
}

func newUndirected[K comparable, T any](hash Hash[K, T], traits *Traits, store Store[K, T]) *undirected[K, T] {
	return &undirected[K, T]{
		hash:   hash,
		traits: traits,
		store:  store,
	}
}

func (u *undirected[K, T]) Traits() *Traits {
	return u.traits
}

func (u *undirected[K, T]) AddVertex(value T, options ...func(*VertexProperties)) error {
	hash := u.hash(value)

	prop := VertexProperties{
		Weight:     0,
		Attributes: make(map[string]string),
	}

	for _, option := range options {
		option(&prop)
	}

	return u.store.AddVertex(hash, value, prop)
}

func (u *undirected[K, T]) Vertex(hash K) (T, error) {
	vertex, _, err := u.store.Vertex(hash)
	return vertex, err
}

func (u *undirected[K, T]) VertexWithProperties(hash K) (T, VertexProperties, error) {
	vertex, prop, err := u.store.Vertex(hash)
	if err != nil {
		return vertex, VertexProperties{}, err
	}

	return vertex, prop, nil
}

func (u *undirected[K, T]) AddEdge(sourceHash, targetHash K, options ...func(*EdgeProperties)) error {
	if _, _, err := u.store.Vertex(sourceHash); err != nil {
		return fmt.Errorf("could not find source vertex with hash %v: %w", sourceHash, err)
	}

	if _, _, err := u.store.Vertex(targetHash); err != nil {
		return fmt.Errorf("could not find target vertex with hash %v: %w", targetHash, err)
	}

	// nolint: govet // false positive err shawdowing
	if _, err := u.Edge(sourceHash, targetHash); !errors.Is(err, ErrEdgeNotFound) {
		return ErrEdgeAlreadyExists
	}

	// If the user opted in to preventing cycles, run a cycle check.
	if u.traits.PreventCycles {
		createsCycle, err := CreatesCycle[K, T](u, sourceHash, targetHash)
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

	if err := u.addEdge(sourceHash, targetHash, edge); err != nil {
		return fmt.Errorf("failed to add edge: %w", err)
	}

	return nil
}

func (u *undirected[K, T]) Edge(sourceHash, targetHash K) (Edge[T], error) {
	// In an undirected graph, since multigraphs aren't supported, the edge AB is the same as BA.
	// Therefore, if source[target] cannot be found, this function also looks for target[source].

	edge, err := u.store.Edge(sourceHash, targetHash)
	if errors.Is(err, ErrEdgeNotFound) {
		edge, err = u.store.Edge(targetHash, sourceHash)
	}

	if err != nil {
		return Edge[T]{}, err
	}

	sourceVertex, _, err := u.store.Vertex(sourceHash)
	if err != nil {
		return Edge[T]{}, err
	}

	targetVertex, _, err := u.store.Vertex(targetHash)
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

func (u *undirected[K, T]) RemoveEdge(source, target K) error {
	if _, err := u.Edge(source, target); err != nil {
		return err
	}

	if err := u.store.RemoveEdge(source, target); err != nil {
		return fmt.Errorf("failed to remove edge from %v to %v: %w", source, target, err)
	}

	if err := u.store.RemoveEdge(target, source); err != nil {
		return fmt.Errorf("failed to remove edge from %v to %v: %w", target, source, err)
	}

	return nil
}

func (u *undirected[K, T]) AdjacencyMap() (map[K]map[K]Edge[K], error) {
	vertices, err := u.store.ListVertices()
	if err != nil {
		return nil, fmt.Errorf("failed to list vertices: %w", err)
	}

	edges, err := u.store.ListEdges()
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

func (u *undirected[K, T]) PredecessorMap() (map[K]map[K]Edge[K], error) {
	return u.AdjacencyMap()
}

func (u *undirected[K, T]) Clone() (Graph[K, T], error) {
	traits := &Traits{
		IsDirected: u.traits.IsDirected,
		IsAcyclic:  u.traits.IsAcyclic,
		IsWeighted: u.traits.IsWeighted,
		IsRooted:   u.traits.IsRooted,
	}

	return &undirected[K, T]{
		hash:   u.hash,
		traits: traits,
		store:  u.store,
	}, nil
}

func (u *undirected[K, T]) Order() (int, error) {
	return u.store.VertexCount()
}

func (u *undirected[K, T]) Size() (int, error) {
	size := 0
	outEdges, err := u.AdjacencyMap()
	if err != nil {
		return 0, fmt.Errorf("failed to get adjacency map: %w", err)
	}
	for _, outEdges := range outEdges {
		size += len(outEdges)
	}

	// Divide by 2 since every add edge operation on undirected graph is counted twice.
	return size / 2, nil
}

func (u *undirected[K, T]) edgesAreEqual(a, b Edge[T]) bool {
	aSourceHash := u.hash(a.Source)
	aTargetHash := u.hash(a.Target)
	bSourceHash := u.hash(b.Source)
	bTargetHash := u.hash(b.Target)

	if aSourceHash == bSourceHash && aTargetHash == bTargetHash {
		return true
	}

	if !u.traits.IsDirected {
		return aSourceHash == bTargetHash && aTargetHash == bSourceHash
	}

	return false
}

func (u *undirected[K, T]) addEdge(sourceHash, targetHash K, edge Edge[K]) error {
	err := u.store.AddEdge(sourceHash, targetHash, edge)
	if err != nil {
		return err
	}

	rEdge := Edge[K]{
		Source: edge.Target,
		Target: edge.Source,
		Properties: EdgeProperties{
			Weight:     edge.Properties.Weight,
			Attributes: edge.Properties.Attributes,
			Data:       edge.Properties.Data,
		},
	}

	err = u.store.AddEdge(targetHash, sourceHash, rEdge)
	if err != nil {
		return err
	}

	return nil
}
