package graph

import (
	"errors"
	"fmt"
	"math"
)

var ErrTargetNotReachable = errors.New("target vertex not reachable from source")

// CreatesCycle determines whether an edge between the given source and target vertices would
// introduce a cycle. It won't create that edge in any case.
//
// A potential edge would create a cycle if the target vertex is also a parent of the source vertex.
// Given a graph A-B-C-D, adding an edge DA would introduce a cycle:
//
//	A -
//	|  |
//	B  |
//	|  |
//	C  |
//	|  |
//	D -
//
// CreatesCycle backtracks the ingoing edges of D, resulting in a reverse walk C-B-A.
func CreatesCycle[K comparable, T any](g Graph[K, T], source, target K) (bool, error) {
	if _, err := g.Vertex(source); err != nil {
		return false, fmt.Errorf("could not get vertex with hash %v: %w", source, err)
	}

	if _, err := g.Vertex(target); err != nil {
		return false, fmt.Errorf("could not get vertex with hash %v: %w", target, err)
	}

	if source == target {
		return true, nil
	}

	predecessorMap, err := g.PredecessorMap()
	if err != nil {
		return false, fmt.Errorf("failed to get predecessor map: %w", err)
	}

	stack := make([]K, 0)
	visited := make(map[K]bool)

	stack = append(stack, source)

	for len(stack) > 0 {
		currentHash := stack[len(stack)-1]
		stack = stack[:len(stack)-1]

		if _, ok := visited[currentHash]; !ok {
			// If the current vertex, e.g. an adjacency of the source vertex, also is the target
			// vertex, an edge between these two would create a cycle.
			if currentHash == target {
				return true, nil
			}
			visited[currentHash] = true

			for adjacency := range predecessorMap[currentHash] {
				stack = append(stack, adjacency)
			}
		}
	}

	return false, nil
}

// ShortestPath computes the shortest path between a source and a target vertex using the edge
// weights and returns the hash values of the vertices forming that path using Dijkstra's algorithm.
// This search runs in O(|V|+|E|log(|V|)) time.
//
// The returned path includes the source and target vertices. If the target cannot be reached
// from the source vertex, ErrTargetNotReachable will be returned. If there are multiple shortest
// paths, an arbitrary one will be returned.
func ShortestPath[K comparable, T any](g Graph[K, T], source, target K) ([]K, error) {
	weights := make(map[K]float64)
	visited := make(map[K]bool)

	weights[source] = 0
	visited[target] = true

	queue := newPriorityQueue[K]()
	adjacencyMap, err := g.AdjacencyMap()
	if err != nil {
		return nil, fmt.Errorf("could not get adjacency map: %w", err)
	}

	for hash := range adjacencyMap {
		if hash != source {
			weights[hash] = math.Inf(1)
			visited[hash] = false
		}

		queue.Push(hash, weights[hash])
	}

	// bestPredecessors stores the best (i.e. cheapest or least-weighted) predecessor for each
	// vertex. If there is an edge AC with weight 4 and an edge BC with weight 2, the best
	// predecessor for C is B.
	bestPredecessors := make(map[K]K)

	for queue.Len() > 0 {
		vertex, _ := queue.Pop()
		hasInfiniteWeight := math.IsInf(weights[vertex], 1)

		for adjacency, edge := range adjacencyMap[vertex] {
			edgeWeight := edge.Properties.Weight

			// Setting the weight to 1 is required for unweighted graphs whose
			// edge weights are 0. Otherwise, all paths would have a sum of 0
			// and a random path would be returned.
			if !g.Traits().IsWeighted {
				edgeWeight = 1
			}

			weight := weights[vertex] + float64(edgeWeight)

			if weight < weights[adjacency] && !hasInfiniteWeight {
				weights[adjacency] = weight
				bestPredecessors[adjacency] = vertex
				queue.UpdatePriority(adjacency, weight)
			}
		}
	}

	// Backtrack the predecessors from target to source. These are the least-weighted edges.
	path := []K{target}
	hashCursor := target

	for hashCursor != source {
		// If hashCursor is not a present key in bestPredecessors, hashCursor is set to the zero
		// value. Without this check, this leads to endless prepending of zeros to the path.
		if _, ok := bestPredecessors[hashCursor]; !ok {
			return nil, ErrTargetNotReachable
		}
		hashCursor = bestPredecessors[hashCursor]
		path = append([]K{hashCursor}, path...)
	}

	return path, nil
}

type sccState[K comparable] struct {
	adjacencyMap map[K]map[K]Edge[K]
	components   [][]K
	stack        []K
	onStack      map[K]bool
	visited      map[K]struct{}
	lowlink      map[K]int
	index        map[K]int
	time         int
}

// StronglyConnectedComponents detects all strongly connected components within the given graph
// and returns the hashes of the vertices shaping these components, so each component is a []K.
//
// The current implementation uses Tarjan's algorithm and runs recursively.
func StronglyConnectedComponents[K comparable, T any](g Graph[K, T]) ([][]K, error) {
	if !g.Traits().IsDirected {
		return nil, errors.New("SCCs can only be detected in directed graphs")
	}

	adjacencyMap, err := g.AdjacencyMap()
	if err != nil {
		return nil, fmt.Errorf("could not get adjacency map: %w", err)
	}

	state := &sccState[K]{
		adjacencyMap: adjacencyMap,
		components:   make([][]K, 0),
		stack:        make([]K, 0),
		onStack:      make(map[K]bool),
		visited:      make(map[K]struct{}),
		lowlink:      make(map[K]int),
		index:        make(map[K]int),
	}

	for hash := range state.adjacencyMap {
		if _, ok := state.visited[hash]; !ok {
			findSCC(hash, state)
		}
	}

	return state.components, nil
}

func findSCC[K comparable](vertexHash K, state *sccState[K]) {
	state.stack = append(state.stack, vertexHash)
	state.onStack[vertexHash] = true
	state.visited[vertexHash] = struct{}{}
	state.index[vertexHash] = state.time
	state.lowlink[vertexHash] = state.time

	state.time++

	for adjacency := range state.adjacencyMap[vertexHash] {
		if _, ok := state.visited[adjacency]; !ok {
			findSCC(adjacency, state)

			smallestLowlink := math.Min(
				float64(state.lowlink[vertexHash]),
				float64(state.lowlink[adjacency]),
			)
			state.lowlink[vertexHash] = int(smallestLowlink)
		} else {
			// If the adjacent vertex already is on the stack, the edge joining the current and the
			// adjacent vertex is a back edge. Therefore, update the vertex' lowlink value to the
			// index of the adjacent vertex if it is smaller than the lowlink value.
			if state.onStack[adjacency] {
				smallestLowlink := math.Min(
					float64(state.lowlink[vertexHash]),
					float64(state.index[adjacency]),
				)
				state.lowlink[vertexHash] = int(smallestLowlink)
			}
		}
	}

	// If the lowlink value of the vertex is equal to its DFS index, this is th head vertex of a
	// strongly connected component, shaped by this vertex and the vertices on the stack.
	if state.lowlink[vertexHash] == state.index[vertexHash] {
		var hash K
		var component []K

		for hash != vertexHash {
			hash = state.stack[len(state.stack)-1]
			state.stack = state.stack[:len(state.stack)-1]
			state.onStack[hash] = false

			component = append(component, hash)
		}

		state.components = append(state.components, component)
	}
}
