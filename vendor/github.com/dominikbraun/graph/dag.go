package graph

import (
	"errors"
	"fmt"
)

// TopologicalSort performs a topological sort on a given graph and returns the vertex hashes in
// topological order. A topological order is a non-unique order of the vertices in a directed graph
// where an edge from vertex A to vertex B implies that vertex A appears before vertex B.
//
// TopologicalSort only works for directed acyclic graphs. The current implementation works non-
// recursively and uses Kahn's algorithm.
func TopologicalSort[K comparable, T any](g Graph[K, T]) ([]K, error) {
	if !g.Traits().IsDirected {
		return nil, fmt.Errorf("topological sort cannot be computed on undirected graph")
	}

	predecessorMap, err := g.PredecessorMap()
	if err != nil {
		return nil, fmt.Errorf("failed to get predecessor map: %w", err)
	}

	queue := make([]K, 0)

	for vertex, predecessors := range predecessorMap {
		if len(predecessors) == 0 {
			queue = append(queue, vertex)
		}
	}

	order := make([]K, 0, len(predecessorMap))
	visited := make(map[K]struct{})

	for len(queue) > 0 {
		currentVertex := queue[0]
		queue = queue[1:]

		if _, ok := visited[currentVertex]; ok {
			continue
		}

		order = append(order, currentVertex)
		visited[currentVertex] = struct{}{}

		for vertex, predecessors := range predecessorMap {
			delete(predecessors, currentVertex)

			if len(predecessors) == 0 {
				queue = append(queue, vertex)
			}
		}
	}

	gOrder, err := g.Order()
	if err != nil {
		return nil, fmt.Errorf("failed to get graph order: %w", err)
	}

	if len(order) != gOrder {
		return nil, errors.New("topological sort cannot be computed on graph with cycles")
	}

	return order, nil
}

// TransitiveReduction returns a new graph with the same vertices and the same reachability as the
// given graph, but with as few edges as possible. This significantly reduces its complexity.
//
// With a time complexity of O(V(V+E)), TransitiveReduction is a very costly operation. Note that
func TransitiveReduction[K comparable, T any](g Graph[K, T]) (Graph[K, T], error) {
	if !g.Traits().IsDirected {
		return nil, fmt.Errorf("transitive reduction cannot be performed on undirected graph")
	}

	transitiveReduction, err := g.Clone()
	if err != nil {
		return nil, fmt.Errorf("failed to clone the graph: %w", err)
	}

	adjacencyMap, err := transitiveReduction.AdjacencyMap()
	if err != nil {
		return nil, fmt.Errorf("failed to get adajcency map: %w", err)
	}

	for vertex, successors := range adjacencyMap {
		// For each direct successor of the current vertex, run a DFS starting from that successor.
		// Then, for each vertex visited in the DFS, inspect all of its edges. Remove the edges that
		// also appear in the edges of the top-level iteration vertex.
		//
		// These edges are redundant because their targets obviously are reachable through the DFS,
		// hence they can be removed from the top-level vertex.
		tOrder, err := transitiveReduction.Order()
		if err != nil {
			return nil, fmt.Errorf("failed to get graph order: %w", err)
		}
		for successor := range successors {
			stack := make([]K, 0, tOrder)
			visited := make(map[K]struct{}, tOrder)
			onStack := make(map[K]bool, tOrder)

			stack = append(stack, successor)

			for len(stack) > 0 {
				current := stack[len(stack)-1]
				stack = stack[:len(stack)-1]

				// If the vertex has already been visited, remove it from the stack and continue
				// with the next vertex. Otherwise, proceed by putting it onto the stack.
				if _, ok := visited[current]; ok {
					onStack[current] = false
					continue
				}

				visited[current] = struct{}{}
				onStack[current] = true
				stack = append(stack, current)

				// Also, if the vertex is a leaf node, remove it from the stack.
				if len(adjacencyMap[current]) == 0 {
					onStack[current] = false
				}

				for adjacency := range adjacencyMap[current] {
					if _, ok := visited[adjacency]; ok {
						if onStack[adjacency] {
							// If the current adjacency is both on the stack and has already been
							// visited, there is a cycle and an error is returned.
							return nil, fmt.Errorf("transitive reduction cannot be performed on graph with cycle")
						}
						continue
					}

					if _, ok := adjacencyMap[vertex][adjacency]; ok {
						_ = transitiveReduction.RemoveEdge(vertex, adjacency)
					}
					stack = append(stack, adjacency)
				}
			}
		}
	}

	return transitiveReduction, nil
}
