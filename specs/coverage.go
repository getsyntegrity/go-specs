package specs

// Coverage is a fixed-size bitmap for lightweight edge coverage (AFL-style).
// Hot-path operations are allocation-free and constant-time.
const coverageSize = 65536

// Coverage tracks hit counts per bucket. Use Hit(edge) to record an edge;
// HasNewCoverage(other) to detect new paths vs a previous snapshot.
type Coverage struct {
	bitmap [coverageSize]uint8
}

// Hit records an edge. Bucket index is edge % len(bitmap). Counts saturate at 255.
func (c *Coverage) Hit(edge uint64) {
	if c == nil {
		return
	}
	idx := edge % uint64(len(c.bitmap))
	if c.bitmap[idx] < 255 {
		c.bitmap[idx]++
	}
}

// HasNewCoverage returns true if any bucket count is greater than in other.
func (c *Coverage) HasNewCoverage(other *Coverage) bool {
	if c == nil {
		return false
	}
	if other == nil {
		return true
	}
	for i := range c.bitmap {
		if c.bitmap[i] > other.bitmap[i] {
			return true
		}
	}
	return false
}

// Reset zeros the bitmap.
func (c *Coverage) Reset() {
	if c == nil {
		return
	}
	for i := range c.bitmap {
		c.bitmap[i] = 0
	}
}

// MergeFrom updates c so each bucket is the maximum of c and other (union of coverage).
func (c *Coverage) MergeFrom(other *Coverage) {
	if c == nil || other == nil {
		return
	}
	for i := range c.bitmap {
		if other.bitmap[i] > c.bitmap[i] {
			c.bitmap[i] = other.bitmap[i]
		}
	}
}
