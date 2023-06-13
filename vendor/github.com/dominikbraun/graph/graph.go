// Package graph provides types and functions for creating generic graph data structures and
// modifying, analyzing, and visualizing them.
package graph

import "errors"

var (
	ErrVertexNotFound      = errors.New("vertex not found")
	ErrVertexAlreadyExists = errors.New("vertex already exists")
	ErrEdgeNotFound        = errors.New("edge not found")
	ErrEdgeAlreadyExists   = errors.New("edge already exists")
	ErrEdgeCreatesCycle    = errors.New("edge would create a cycle")
)

// Graph represents a generic graph data structure consisting of vertices and edges. Its vertices
// are of type T, and each vertex is identified by a hash of type K.
type Graph[K comparable, T any] interface {
	// Traits returns the graph's traits. Those traits must be set when creating a graph using New.
	Traits() *Traits

	// AddVertex creates a new vertex in the graph. If the vertex already exists in the graph,
	// ErrVertexAlreadyExists will be returned if no custom Store implementation is used.
	//
	// AddVertex accepts a variety of functional options to set further edge details such as the
	// weight or an attribute:
	//
	//	_ = graph.AddVertex("A", "B", graph.VertexWeight(4), graph.VertexAttribute("label", "my-label"))
	//
	AddVertex(value T, options ...func(*VertexProperties)) error

	// Vertex returns the vertex with the given hash or ErrVertexNotFound if it doesn't exist.
	Vertex(hash K) (T, error)

	// VertexWithProperties returns the vertex with the given hash along with its properties or
	// ErrVertexNotFound if it doesn't exist.
	VertexWithProperties(hash K) (T, VertexProperties, error)

	// AddEdge creates an edge between the source and the target vertex. If the Directed option has
	// been called on the graph, this is a directed edge. If either vertex can't be found,
	// ErrVertexNotFound will be returned. If the edge already exists, ErrEdgeAlreadyExists will be
	// returned. If cycle prevention has been activated using PreventCycles and adding the edge
	// would create a cycle, ErrEdgeCreatesCycle will be returned.
	//
	// AddEdge accepts a variety of functional options to set further edge details such as the
	// weight or an attribute:
	//
	//	_ = graph.AddEdge("A", "B", graph.EdgeWeight(4), graph.EdgeAttribute("label", "my-label"))
	//
	AddEdge(sourceHash, targetHash K, options ...func(*EdgeProperties)) error

	// Edge returns the edge joining two given vertices or an error if the edge doesn't exist. In an
	// undirected graph, an edge with swapped source and target vertices does match.
	//
	// If the edge doesn't exist, ErrEdgeNotFound will be returned.
	Edge(sourceHash, targetHash K) (Edge[T], error)

	// RemoveEdge removes the edge between the given source and target vertices. If the edge doesn't
	// exist, ErrEdgeNotFound will be returned if no custom Store implementation is used.
	RemoveEdge(source, target K) error

	// AdjacencyMap computes and returns an adjacency map containing all vertices in the graph.
	//
	// There is an entry for each vertex, and each of those entries is another map whose keys are
	// the hash values of the adjacent vertices. The value is an Edge instance that stores the
	// source and target hash values (these are the same as the map keys) as well as edge metadata.
	//
	// For a graph with edges AB and AC, the adjacency map would look as follows:
	//
	//	map[string]map[string]Edge[string]{
	//		"A": map[string]Edge[string]{
	//			"B": {Source: "A", Target: "B"}
	//			"C": {Source: "A", Target: "C"}
	//		}
	//	}
	//
	// This design makes AdjacencyMap suitable for a wide variety of scenarios and demands.
	AdjacencyMap() (map[K]map[K]Edge[K], error)

	// PredecessorMap computes and returns a predecessors map containing all vertices in the graph.
	//
	// The map layout is the same as for AdjacencyMap.
	//
	// For an undirected graph, PredecessorMap is the same as AdjacencyMap. For a directed graph,
	// PredecessorMap is the complement of AdjacencyMap. This is because in a directed graph, only
	// vertices joined by an outgoing edge are considered adjacent to the current vertex, whereas
	// predecessors are the vertices joined by an ingoing edge.
	PredecessorMap() (map[K]map[K]Edge[K], error)

	// Clone creates an independent deep copy of the graph and returns that cloned graph.
	Clone() (Graph[K, T], error)

	// Order computes and returns the number of vertices in the graph.
	Order() (int, error)

	// Size computes and returns the number of edges in the graph.
	Size() (int, error)
}

// Edge represents a graph edge with a source and target vertex as well as a weight, which has the
// same value for all edges in an unweighted graph. Even though the vertices are referred to as
// source and target, whether the graph is directed or not is determined by its traits.
type Edge[T any] struct {
	Source     T
	Target     T
	Properties EdgeProperties
}

// EdgeProperties represents a set of properties that each edge possesses. They can be set when
// adding a new edge using the functional options provided by this library:
//
//	g.AddEdge("A", "B", graph.EdgeWeight(2), graph.EdgeAttribute("color", "red"))
//
// The example above will create an edge with weight 2 and a "color" attribute with value "red".
type EdgeProperties struct {
	Attributes map[string]string
	Weight     int
	Data       any
}

// Hash is a hashing function that takes a vertex of type T and returns a hash value of type K.
//
// Every graph has a hashing function and uses that function to retrieve the hash values of its
// vertices. You can either use one of the predefined hashing functions, or, if you want to store a
// custom data type, provide your own function:
//
//	cityHash := func(c City) string {
//		return c.Name
//	}
//
// The cityHash function returns the city name as a hash value. The types of T and K, in this case
// City and string, also define the types T and K of the graph.
type Hash[K comparable, T any] func(T) K

// New creates a new graph with vertices of type T, identified by hash values of type K. These hash
// values will be obtained using the provided hash function (see Hash).
//
// For primitive vertex values, you may use the predefined hashing functions. As an example, this
// graph stores integer vertices:
//
//	g := graph.New(graph.IntHash)
//	_ = g.AddVertex(1)
//	_ = g.AddVertex(2)
//	_ = g.AddVertex(3)
//
// The provided IntHash hashing function takes an integer and uses it as a hash value at the same
// time. In a more complex scenario with custom objects, you should define your own function:
//
//	type City struct {
//		Name string
//	}
//
//	cityHash := func(c City) string {
//		return c.Name
//	}
//
//	g := graph.New(cityHash)
//	_ = g.AddVertex(london)
//
// This graph will store vertices of type City, identified by hashes of type string. Both type
// parameters can be inferred from the hashing function.
//
// All traits of the graph can be set using the predefined functional options. They can be combined
// arbitrarily. This example creates a directed acyclic graph:
//
//	g := graph.New(graph.IntHash, graph.Directed(), graph.Acyclic())
//
// Which Graph implementation will be returned depends on these traits.
func New[K comparable, T any](hash Hash[K, T], options ...func(*Traits)) Graph[K, T] {
	return NewWithStore(hash, newMemoryStore[K, T](), options...)
}

// NewWithStore creates a new graph same as New, but uses the provided store instead of the default
// memory store.
func NewWithStore[K comparable, T any](hash Hash[K, T], store Store[K, T], options ...func(*Traits)) Graph[K, T] {
	var p Traits

	for _, option := range options {
		option(&p)
	}

	if p.IsDirected {
		return newDirected(hash, &p, store)
	}

	return newUndirected(hash, &p, store)
}

// StringHash is a hashing function that accepts a string and uses that exact string as a hash
// value. Using it as Hash will yield a Graph[string, string].
func StringHash(v string) string {
	return v
}

// IntHash is a hashing function that accepts an integer and uses that exact integer as a hash
// value. Using it as Hash will yield a Graph[int, int].
func IntHash(v int) int {
	return v
}

// EdgeWeight returns a function that sets the weight of an edge to the given weight. This is a
// functional option for the Edge and AddEdge methods.
func EdgeWeight(weight int) func(*EdgeProperties) {
	return func(e *EdgeProperties) {
		e.Weight = weight
	}
}

// EdgeAttribute returns a function that adds the given key-value pair to the attributes of an
// edge. This is a functional option for the Edge and AddEdge methods.
func EdgeAttribute(key, value string) func(*EdgeProperties) {
	return func(e *EdgeProperties) {
		e.Attributes[key] = value
	}
}

// EdgeData returns a function that sets the data of an edge to the given value. This is a
// functional option for the Edge and AddEdge methods.
func EdgeData(data any) func(*EdgeProperties) {
	return func(e *EdgeProperties) {
		e.Data = data
	}
}

// VertexProperties represents a set of properties that each vertex has. They can be set when adding
// a new vertex using the functional options provided by this library:
//
//	_ = g.AddVertex("A", "B", graph.VertexWeight(2), graph.VertexAttribute("color", "red"))
//
// The example above will create a vertex with weight 2 and a "color" attribute with value "red".
type VertexProperties struct {
	Attributes map[string]string
	Weight     int
}

// VertexWeight returns a function that sets the weight of a vertex to the given weight. This is a
// functional option for the Vertex and AddVertex methods.
func VertexWeight(weight int) func(*VertexProperties) {
	return func(e *VertexProperties) {
		e.Weight = weight
	}
}

// VertexAttribute returns a function that adds the given key-value pair to the attributes of a
// vertex. This is a functional option for the Vertex and AddVertex methods.
func VertexAttribute(key, value string) func(*VertexProperties) {
	return func(e *VertexProperties) {
		e.Attributes[key] = value
	}
}
