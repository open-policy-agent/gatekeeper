# ![dominikbraun/graph](img/logo.svg)

A library for creating generic graph data structures and modifying, analyzing, and visualizing them.

# Features

* Generic vertices of any type, such as `int` or `City`.
* Graph traits with corresponding validations, such as cycle checks in acyclic graphs.
* Algorithms for finding paths or components, such as shortest paths or strongly connected components.
* Algorithms for transformations and representations, such as transitive reduction or topological order.
* Algorithms for non-recursive graph traversal, such as DFS or BFS.
* Vertices and edges with optional metadata, such as weights or custom attributes.
* Visualization of graphs using the DOT language and Graphviz.
* Integrate any storage backend by using your own `Store` implementation.
* Extensive tests with ~90% coverage, and zero dependencies.

> Status: Because `graph` is in version 0, the public API shouldn't be considered stable.

> This README may contain unreleased changes. Check out the [latest documentation](https://pkg.go.dev/github.com/dominikbraun/graph).

# Getting started

```
go get github.com/dominikbraun/graph
```

# Quick examples

## Create a graph of integers

![graph of integers](img/simple.svg)

```go
g := graph.New(graph.IntHash)

_ = g.AddVertex(1)
_ = g.AddVertex(2)
_ = g.AddVertex(3)
_ = g.AddVertex(4)
_ = g.AddVertex(5)

_ = g.AddEdge(1, 2)
_ = g.AddEdge(1, 4)
_ = g.AddEdge(2, 3)
_ = g.AddEdge(2, 4)
_ = g.AddEdge(2, 5)
_ = g.AddEdge(3, 5)
```

## Create a directed acyclic graph of integers

![directed acyclic graph](img/dag.svg)

```go
g := graph.New(graph.IntHash, graph.Directed(), graph.Acyclic())

_ = g.AddVertex(1)
_ = g.AddVertex(2)
_ = g.AddVertex(3)
_ = g.AddVertex(4)

_ = g.AddEdge(1, 2)
_ = g.AddEdge(1, 3)
_ = g.AddEdge(2, 3)
_ = g.AddEdge(2, 4)
_ = g.AddEdge(3, 4)
```

## Create a graph of a custom type

To understand this example in detail, see the [concept of hashes](#hashes).

```go
type City struct {
    Name string
}

cityHash := func(c City) string {
    return c.Name
}

g := graph.New(cityHash)

_ = g.AddVertex(london)
```

## Create a weighted graph

![weighted graph](img/cities.svg)

```go
g := graph.New(cityHash, graph.Weighted())

_ = g.AddVertex(london)
_ = g.AddVertex(munich)
_ = g.AddVertex(paris)
_ = g.AddVertex(madrid)

_ = g.AddEdge("london", "munich", graph.EdgeWeight(3))
_ = g.AddEdge("london", "paris", graph.EdgeWeight(2))
_ = g.AddEdge("london", "madrid", graph.EdgeWeight(5))
_ = g.AddEdge("munich", "madrid", graph.EdgeWeight(6))
_ = g.AddEdge("munich", "paris", graph.EdgeWeight(2))
_ = g.AddEdge("paris", "madrid", graph.EdgeWeight(4))
```

## Perform a Depth-First Search

This example traverses and prints all vertices in the graph in DFS order.

![depth-first search](img/dfs.svg)

```go
g := graph.New(graph.IntHash, graph.Directed())

_ = g.AddVertex(1)
_ = g.AddVertex(2)
_ = g.AddVertex(3)
_ = g.AddVertex(4)

_ = g.AddEdge(1, 2)
_ = g.AddEdge(1, 3)
_ = g.AddEdge(3, 4)

_ = graph.DFS(g, 1, func(value int) bool {
    fmt.Println(value)
    return false
})
```

```
1 3 4 2
```

## Find strongly connected components

![strongly connected components](img/scc.svg)

```go
g := graph.New(graph.IntHash)

// Add vertices and edges ...

scc, _ := graph.StronglyConnectedComponents(g)

fmt.Println(scc)
```

```
[[1 2 5] [3 4 8] [6 7]]
```

## Find the shortest path

![shortest path algorithm](img/dijkstra.svg)

```go
g := graph.New(graph.StringHash, graph.Weighted())

// Add vertices and weighted edges ...

path, _ := graph.ShortestPath(g, "A", "B")

fmt.Println(path)
```

```
[A C E B]
```

## Perform a topological sort

![topological sort](img/topological-sort.svg)

```go
g := graph.New(graph.IntHash, graph.Directed(), graph.PreventCycles())

// Add vertices and edges ...

order, _ := graph.TopologicalSort(g)

fmt.Println(order)
```

```
[1 2 3 4 5]
```

## Perform a transitive reduction

![transitive reduction](img/transitive-reduction-before.svg)

```go
g := graph.New(graph.StringHash, graph.Directed(), graph.PreventCycles())

// Add vertices and edges ...

transitiveReduction, _ := graph.TransitiveReduction(g)
```

![transitive reduction](img/transitive-reduction-after.svg)

## Prevent the creation of cycles

![cycle checks](img/cycles.svg)

```go
g := graph.New(graph.IntHash, graph.PreventCycles())

_ = g.AddVertex(1)
_ = g.AddVertex(2)
_ = g.AddVertex(3)

_ = g.AddEdge(1, 2)
_ = g.AddEdge(1, 3)

if err := g.AddEdge(2, 3); err != nil {
    panic(err)
}
```

```
panic: an edge between 2 and 3 would introduce a cycle
```

## Visualize a graph using Graphviz

The following example will generate a DOT description for `g` and write it into the given file.

```go
g := graph.New(graph.IntHash, graph.Directed())

_ = g.AddVertex(1)
_ = g.AddVertex(2)
_ = g.AddVertex(3)

_ = g.AddEdge(1, 2)
_ = g.AddEdge(1, 3)

file, _ := os.Create("./mygraph.gv")
_ = draw.DOT(g, file)
```

To generate an SVG from the created file using Graphviz, use a command such as the following:

```
dot -Tsvg -O mygraph.gv
```

### Draw a graph as in this documentation

![simple graph](img/simple.svg)

This graph has been rendered using the following program:

```go
package main

import (
	"os"

	"github.com/dominikbraun/graph"
	"github.com/dominikbraun/graph/draw"
)

func main() {
	g := graph.New(graph.IntHash)

	_ = g.AddVertex(1, graph.VertexAttribute("colorscheme", "blues3"), graph.VertexAttribute("style", "filled"), graph.VertexAttribute("color", "2"), graph.VertexAttribute("fillcolor", "1"))
	_ = g.AddVertex(2, graph.VertexAttribute("colorscheme", "greens3"), graph.VertexAttribute("style", "filled"), graph.VertexAttribute("color", "2"), graph.VertexAttribute("fillcolor", "1"))
	_ = g.AddVertex(3, graph.VertexAttribute("colorscheme", "purples3"), graph.VertexAttribute("style", "filled"), graph.VertexAttribute("color", "2"), graph.VertexAttribute("fillcolor", "1"))
	_ = g.AddVertex(4, graph.VertexAttribute("colorscheme", "ylorbr3"), graph.VertexAttribute("style", "filled"), graph.VertexAttribute("color", "2"), graph.VertexAttribute("fillcolor", "1"))
	_ = g.AddVertex(5, graph.VertexAttribute("colorscheme", "reds3"), graph.VertexAttribute("style", "filled"), graph.VertexAttribute("color", "2"), graph.VertexAttribute("fillcolor", "1"))

	_ = g.AddEdge(1, 2)
	_ = g.AddEdge(1, 4)
	_ = g.AddEdge(2, 3)
	_ = g.AddEdge(2, 4)
	_ = g.AddEdge(2, 5)
	_ = g.AddEdge(3, 5)

	file, _ := os.Create("./simple.gv")
	_ = draw.DOT(g, file)
}
```

It has been rendered using the `neato` engine:

```
dot -Tsvg -Kneato -O simple.gv
```

The example uses the [Brewer color scheme](https://graphviz.org/doc/info/colors.html#brewer) supported by Graphviz.

## Storing edge attributes

Edges may have one or more attributes which can be used to store metadata. Attributes will be taken
into account when [visualizing a graph](#visualize-a-graph-using-graphviz). For example, this edge
will be rendered in red color:

```go
_ = g.AddEdge(1, 2, graph.EdgeAttribute("color", "red"))
```

To get an overview of all supported attributes, take a look at the
[DOT documentation](https://graphviz.org/doc/info/attrs.html).

The stored attributes can be retrieved by getting the edge and accessing the `Properties.Attributes`
field.

```go
edge, _ := g.Edge(1, 2)
color := edge.Properties.Attributes["color"] 
```

## Storing edge data

It is also possible to store arbitrary data inside edges, not just key-value string pairs. This data
is of type `any`.

```go
_  = g.AddEdge(1, 2, graph.EdgeData(myData))
```

The stored data can be retrieved by getting the edge and accessing the `Properties.Data` field.

```go
edge, _ := g.Edge(1, 2)
myData := edge.Properties.Data 
```

## Storing vertex attributes

Vertices may have one or more attributes which can be used to store metadata. Attributes will be
taken into account when [visualizing a graph](#visualize-a-graph-using-graphviz). For example, this
vertex will be rendered in red color:

```go
_ = g.AddVertex(1, graph.VertexAttribute("style", "filled"))
```

The stored data can be retrieved by getting the vertex using `VertexWithProperties` and accessing
the `Attributes` field.

```go
vertex, properties, _ := g.VertexWithProperties(1)
style := properties.Attributes["style"]
```

To get an overview of all supported attributes, take a look at the
[DOT documentation](https://graphviz.org/doc/info/attrs.html).

## Store the graph in a custom storage

You can integrate any storage backend by implementing the `Store` interface and initializing a new
graph with it:

```go
g := graph.NewWithStore(graph.IntHash, myStore)
```

To implement the `Store` interface appropriately, take a look at the [documentation](https://pkg.go.dev/github.com/dominikbraun/graph#Store).
[`graph-sql`](https://github.com/dominikbraun/graph-sql) is a ready-to-use SQL store implementation.

# Concepts

## Hashes

A graph consists of nodes (or vertices) of type `T`, which are identified by a hash value of type
`K`. The hash value is obtained using the hashing function passed to `graph.New`.

### Primitive types

For primitive types such as `string` or `int`, you may use a predefined hashing function such as
`graph.IntHash` – a function that takes an integer and uses it as a hash value at the same time:

```go
g := graph.New(graph.IntHash)
```

> This also means that only one vertex with a value like `5` can exist in the graph if
> `graph.IntHash` used.

### Custom types

For storing custom data types, you need to provide your own hashing function. This example function
takes a `City` and returns the city name as an unique hash value:

```go
cityHash := func(c City) string {
    return c.Name
}
```

Creating a graph using this hashing function will yield a graph with vertices of type `City`
identified by hash values of type `string`.

```go
g := graph.New(cityHash)
```

## Traits

The behavior of a graph, for example when adding or retrieving edges, depends on its traits. You
can set the graph's traits using the functional options provided by this library:

```go
g := graph.New(graph.IntHash, graph.Directed(), graph.Weighted())
```

# Documentation

The full documentation is available at [pkg.go.dev](https://pkg.go.dev/github.com/dominikbraun/graph).
