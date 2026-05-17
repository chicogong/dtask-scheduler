package scheduler

// tagIndex is an inverted index mapping a resource tag to the set of worker IDs
// that carry it. It accelerates tag-based filtering: instead of scanning every
// worker (O(N)), the scheduler intersects the (typically small) candidate sets
// of the required tags.
//
// tagIndex is NOT safe for concurrent use. Callers must hold the owning
// StateManager's lock.
type tagIndex struct {
	index map[string]map[string]struct{} // tag -> set of worker IDs
}

// newTagIndex creates an empty tag index.
func newTagIndex() *tagIndex {
	return &tagIndex{index: make(map[string]map[string]struct{})}
}

// add registers a worker under each of the given tags.
func (ti *tagIndex) add(workerID string, tags []string) {
	for _, tag := range tags {
		set, ok := ti.index[tag]
		if !ok {
			set = make(map[string]struct{})
			ti.index[tag] = set
		}
		set[workerID] = struct{}{}
	}
}

// remove unregisters a worker from each of the given tags, dropping tag entries
// that become empty.
func (ti *tagIndex) remove(workerID string, tags []string) {
	for _, tag := range tags {
		set, ok := ti.index[tag]
		if !ok {
			continue
		}
		delete(set, workerID)
		if len(set) == 0 {
			delete(ti.index, tag)
		}
	}
}

// update moves a worker from its previous tag set to a new one. It is a cheap
// no-op when the tags are unchanged, which is the common case across heartbeats.
func (ti *tagIndex) update(workerID string, oldTags, newTags []string) {
	if sameTags(oldTags, newTags) {
		return
	}
	ti.remove(workerID, oldTags)
	ti.add(workerID, newTags)
}

// candidates returns the IDs of workers carrying ALL required tags. The result
// order is unspecified. It returns nil when required is empty (callers should
// fall back to listing every worker) or when no worker matches.
func (ti *tagIndex) candidates(required []string) []string {
	if len(required) == 0 {
		return nil
	}

	// Start the intersection from the smallest tag set to minimize work.
	smallest := required[0]
	for _, tag := range required[1:] {
		if len(ti.index[tag]) < len(ti.index[smallest]) {
			smallest = tag
		}
	}

	base, ok := ti.index[smallest]
	if !ok {
		return nil
	}

	result := make([]string, 0, len(base))
	for workerID := range base {
		if ti.hasAll(workerID, required) {
			result = append(result, workerID)
		}
	}
	return result
}

// hasAll reports whether the worker is present in every required tag's set.
func (ti *tagIndex) hasAll(workerID string, required []string) bool {
	for _, tag := range required {
		set, ok := ti.index[tag]
		if !ok {
			return false
		}
		if _, ok := set[workerID]; !ok {
			return false
		}
	}
	return true
}

// sameTags reports whether two tag slices are element-wise equal. Workers
// normally report tags in a stable order, so this fast path avoids index churn.
func sameTags(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
