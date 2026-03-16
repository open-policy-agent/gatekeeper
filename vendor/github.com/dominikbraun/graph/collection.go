package graph

import (
	"container/heap"
	"errors"
)

// priorityQueue is a priority queue implementation for minimum priorities, meaning that smaller
// values will be prioritized. It maintains a list of priority items in descending order.
//
// This implementation is built on top of heap.Interface with some adjustments to comply with the
// usage of generics.
type priorityQueue[T comparable] struct {
	items *minHeap[T]
	cache map[T]*priorityItem[T]
}

// priorityItem is an item in the priority queue, consisting of a priority and an actual value.
type priorityItem[T comparable] struct {
	value    T
	priority float64
	// index is used internally by heap.Interface to re-organize items in the queue.
	index int
}

func newPriorityQueue[T comparable]() *priorityQueue[T] {
	return &priorityQueue[T]{
		items: &minHeap[T]{},
		cache: map[T]*priorityItem[T]{},
	}
}

func (p *priorityQueue[T]) Len() int {
	return p.items.Len()
}

// Push pushes a new item with the given priority into the queue.
func (p *priorityQueue[T]) Push(item T, priority float64) {
	if _, ok := p.cache[item]; ok {
		return
	}

	newItem := &priorityItem[T]{
		value:    item,
		priority: priority,
		index:    0,
	}

	heap.Push(p.items, newItem)
	p.cache[item] = newItem
}

// Pop returns the item with the smallest priority from the queue and removes that item.
func (p *priorityQueue[T]) Pop() (T, error) {
	if len(*p.items) == 0 {
		var empty T
		return empty, errors.New("priority queue is empty")
	}

	item := heap.Pop(p.items).(*priorityItem[T])
	delete(p.cache, item.value)

	return item.value, nil
}

// UpdatePriority updates the priority of a given item to the given priority. The item must be
// pushed into the queue first. If the item doesn't exist, nothing happens.
func (p *priorityQueue[T]) UpdatePriority(item T, priority float64) {
	targetItem, ok := p.cache[item]
	if !ok {
		return
	}

	targetItem.priority = priority
	heap.Fix(p.items, targetItem.index)
}

// minHeap is a binary min heap that implements heap.Interface.
type minHeap[T comparable] []*priorityItem[T]

func (m *minHeap[T]) Len() int {
	return len(*m)
}

func (m *minHeap[T]) Less(i, j int) bool {
	return (*m)[i].priority < (*m)[j].priority
}

func (m *minHeap[T]) Swap(i, j int) {
	(*m)[i], (*m)[j] = (*m)[j], (*m)[i]
	(*m)[i].index = i
	(*m)[j].index = j
}

func (m *minHeap[T]) Push(item interface{}) {
	i := item.(*priorityItem[T])
	i.index = len(*m)
	*m = append(*m, i)
}

func (m *minHeap[T]) Pop() interface{} {
	old := *m
	item := old[len(old)-1]
	*m = old[:len(old)-1]

	return item
}
