package graph

import "sync"

// Store represents a storage for vertices and edges. The graph library provides an in-memory store
// by default and accepts any Store implementation to work with - for example, an SQL store.
//
// When implementing your own Store, make sure the individual methods and their behavior adhere to
// this documentation. Otherwise, the graphs aren't guaranteed to behave as expected.
type Store[K comparable, T any] interface {
	// AddVertex should add the given vertex with the given hash value and vertex properties to the
	// graph. If the vertex already exists, it is up to you whether ErrVertexAlreadyExists or no
	// error should be returned.
	AddVertex(hash K, value T, properties VertexProperties) error

	// Vertex should return the vertex and vertex properties with the given hash value. If the
	// vertex doesn't exist, ErrVertexNotFound should be returned.
	Vertex(hash K) (T, VertexProperties, error)

	// ListVertices should return all vertices in the graph in a slice.
	ListVertices() ([]K, error)

	// VertexCount should return the number of vertices in the graph. This should be equal to the
	// length of the slice returned by ListVertices.
	VertexCount() (int, error)

	// AddEdge should add an edge between the vertices with the given source and target hashes.
	//
	// If either vertex doesn't exit, ErrVertexNotFound should be returned for the respective
	// vertex. If the edge already exists, ErrEdgeAlreadyExists should be returned.
	AddEdge(sourceHash, targetHash K, edge Edge[K]) error

	// RemoveEdge should remove the edge between the vertices with the given source and target
	// hashes.
	//
	// If either vertex doesn't exist, it is up to you whether ErrVertexNotFound or no error should
	// be returned. If the edge doesn't exist, it is up to you whether ErrEdgeNotFound or no error
	// should be returned.
	RemoveEdge(sourceHash, targetHash K) error

	// Edge should return the edge joining the vertices with the given hash values. It should
	// exclusively look for an edge between the source and the target vertex, not vice versa. The
	// graph implementation does this for undirected graphs itself.
	//
	// Note that unlike Graph.Edge, this function is supposed to return an Edge[K], i.e. an edge
	// that only contains the vertex hashes instead of the vertices themselves.
	//
	// If the edge doesn't exist, ErrEdgeNotFound should be returned.
	Edge(sourceHash, targetHash K) (Edge[K], error)

	// ListEdges should return all edges in the graph in a slice.
	ListEdges() ([]Edge[K], error)
}

type memoryStore[K comparable, T any] struct {
	lock             sync.RWMutex
	vertices         map[K]T
	vertexProperties map[K]VertexProperties

	// outEdges and inEdges store all outgoing and ingoing edges for all vertices. For O(1) access,
	// these edges themselves are stored in maps whose keys are the hashes of the target vertices.
	outEdges map[K]map[K]Edge[K] // source -> target
	inEdges  map[K]map[K]Edge[K] // target -> source
}

func newMemoryStore[K comparable, T any]() Store[K, T] {
	return &memoryStore[K, T]{
		vertices:         make(map[K]T),
		vertexProperties: make(map[K]VertexProperties),
		outEdges:         make(map[K]map[K]Edge[K]),
		inEdges:          make(map[K]map[K]Edge[K]),
	}
}

func (s *memoryStore[K, T]) AddVertex(k K, t T, p VertexProperties) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	if _, ok := s.vertices[k]; ok {
		return ErrVertexAlreadyExists
	}

	s.vertices[k] = t
	s.vertexProperties[k] = p

	return nil
}

func (s *memoryStore[K, T]) ListVertices() ([]K, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	var hashes []K
	for k := range s.vertices {
		hashes = append(hashes, k)
	}

	return hashes, nil
}

func (s *memoryStore[K, T]) VertexCount() (int, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	return len(s.vertices), nil
}

func (s *memoryStore[K, T]) Vertex(k K) (T, VertexProperties, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	var v T
	var ok bool
	v, ok = s.vertices[k]
	if !ok {
		return v, VertexProperties{}, ErrVertexNotFound
	}

	p := s.vertexProperties[k]
	return v, p, nil
}

func (s *memoryStore[K, T]) AddEdge(sourceHash, targetHash K, edge Edge[K]) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	if _, ok := s.outEdges[sourceHash]; !ok {
		s.outEdges[sourceHash] = make(map[K]Edge[K])
	}

	s.outEdges[sourceHash][targetHash] = edge

	if _, ok := s.inEdges[targetHash]; !ok {
		s.inEdges[targetHash] = make(map[K]Edge[K])
	}

	s.inEdges[targetHash][sourceHash] = edge

	return nil
}

func (s *memoryStore[K, T]) RemoveEdge(sourceHash, targetHash K) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	delete(s.inEdges[sourceHash], targetHash)
	delete(s.outEdges[sourceHash], targetHash)
	return nil
}

func (s *memoryStore[K, T]) Edge(sourceHash, targetHash K) (Edge[K], error) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	sourceEdges, ok := s.outEdges[sourceHash]
	if !ok {
		return Edge[K]{}, ErrEdgeNotFound
	}

	edge, ok := sourceEdges[targetHash]
	if !ok {
		return Edge[K]{}, ErrEdgeNotFound
	}

	return edge, nil
}

func (s *memoryStore[K, T]) ListEdges() ([]Edge[K], error) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	res := make([]Edge[K], 0)
	for _, edges := range s.outEdges {
		for _, edge := range edges {
			res = append(res, edge)
		}
	}
	return res, nil
}
