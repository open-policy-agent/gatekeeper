package client

import (
	"fmt"
	"sort"
	"sync"

	"github.com/open-policy-agent/frameworks/constraint/pkg/client/errors"
	"github.com/open-policy-agent/frameworks/constraint/pkg/core/constraints"
)

// matcherKey uniquely identifies a Matcher.
// For a given Constraint (uniquely identified by Kind/Name), there is at most
// one Matcher for each Target.
type matcherKey struct {
	target string
	kind   string
	name   string
}

// constraintMatchers tracks the Matchers for each Constraint.
// Filters Constraints relevant to a passed review.
// Threadsafe.
type constraintMatchers struct {
	// matchers is the set of Constraint matchers by their Target, Kind, and Name.
	// matchers is a map from Target to a map from Kind to a map from Name to matcher.
	matchers map[string]targetMatchers

	mtx sync.RWMutex
}

// Add inserts the Matcher for the Constraint with kind and name.
// Replaces the current Matcher if one already exists.
func (c *constraintMatchers) Add(key matcherKey, matcher constraints.Matcher) {
	c.mtx.Lock()
	defer c.mtx.Unlock()

	if c.matchers == nil {
		c.matchers = make(map[string]targetMatchers)
	}

	target := c.matchers[key.target]
	target.Add(key, matcher)

	c.matchers[key.target] = target
}

// Remove deletes the Matcher for the Constraint with kind and name.
// Returns normally if no entry for the Constraint existed.
func (c *constraintMatchers) Remove(key matcherKey) {
	c.mtx.Lock()
	defer c.mtx.Unlock()

	if len(c.matchers) == 0 {
		return
	}

	target, ok := c.matchers[key.target]
	if !ok {
		return
	}

	target.Remove(key)

	if len(target.matchers) == 0 {
		delete(c.matchers, key.target)
	} else {
		c.matchers[key.target] = target
	}
}

// RemoveKind removes all Matchers for Constraints with kind.
// Returns normally if no entry for the kind exists for any target.
func (c *constraintMatchers) RemoveKind(kind string) {
	c.mtx.Lock()
	defer c.mtx.Unlock()

	if len(c.matchers) == 0 {
		return
	}

	for name, target := range c.matchers {
		target.RemoveKind(kind)

		if len(target.matchers) == 0 {
			// It is safe to delete keys from a map while traversing it.
			delete(c.matchers, name)
		} else {
			c.matchers[name] = target
		}
	}

	delete(c.matchers, kind)
}

// ConstraintsFor returns the set of Constraints which should run against review
// according to their Matchers. Returns a map from Kind to the names of the
// Constraints of that Kind which should be run against review.
//
// Returns errors for each Constraint which was unable to properly run match
// criteria.
func (c *constraintMatchers) ConstraintsFor(targetName string, review interface{}) (map[string][]string, error) {
	c.mtx.RLock()
	defer c.mtx.RUnlock()

	result := make(map[string][]string)
	target := c.matchers[targetName]
	errs := errors.ErrorMap{}

	for kind, kindMatchers := range target.matchers {
		var resultKindMatchers []string

		for name, matcher := range kindMatchers {
			if matches, err := matcher.Match(review); err != nil {
				// key uniquely identifies the Constraint whose matcher was unable to
				// run, for use in debugging.
				key := fmt.Sprintf("%s %s %s", targetName, kind, name)
				errs[key] = err
			} else if matches {
				resultKindMatchers = append(resultKindMatchers, name)
			}
		}

		sort.Strings(resultKindMatchers)
		result[kind] = resultKindMatchers
	}

	if len(errs) > 0 {
		return nil, &errs
	}

	return result, nil
}

// targetMatchers are the Matchers for Constraints for a specific target.
// Not threadsafe.
type targetMatchers struct {
	matchers map[string]map[string]constraints.Matcher
}

func (t *targetMatchers) Add(key matcherKey, matcher constraints.Matcher) {
	if t.matchers == nil {
		t.matchers = make(map[string]map[string]constraints.Matcher)
	}

	kindMatchers := t.matchers[key.kind]
	if kindMatchers == nil {
		kindMatchers = make(map[string]constraints.Matcher)
	}

	kindMatchers[key.name] = matcher
	t.matchers[key.kind] = kindMatchers
}

func (t *targetMatchers) Remove(key matcherKey) {
	kindMatchers, ok := t.matchers[key.kind]
	if !ok {
		return
	}

	delete(kindMatchers, key.name)

	// Remove empty parents to avoid memory leaks.
	if len(kindMatchers) == 0 {
		delete(t.matchers, key.kind)
	} else {
		t.matchers[key.kind] = kindMatchers
	}
}

func (t *targetMatchers) RemoveKind(kind string) {
	delete(t.matchers, kind)
}
