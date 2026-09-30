package property_test

import (
	"testing"

	"github.com/getsyntegrity/go-specs/assert"
	"github.com/getsyntegrity/go-specs/property"
	"pgregory.net/rapid"
)

// reverseTwiceIsIdentity is a true invariant. Under plain `go test` FuzzReverseTwice replays its seeds
// and any file in testdata/fuzz/FuzzReverseTwice; `go test -fuzz=FuzzReverseTwice` searches for more.
func reverseTwiceIsIdentity(p *property.T) {
	s := property.Draw(p, rapid.String(), "s")
	r := []rune(s)
	for i, j := 0, len(r)-1; i < j; i, j = i+1, j-1 {
		r[i], r[j] = r[j], r[i]
	}
	for i, j := 0, len(r)-1; i < j; i, j = i+1, j-1 {
		r[i], r[j] = r[j], r[i]
	}
	p.Expect(string(r)).To(assert.Equal(s))
}

func FuzzReverseTwice(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte("seed corpus entry"))
	f.Add([]byte{0xff, 0x00, 0x7f, 0x80})
	f.Fuzz(property.Fuzz(reverseTwiceIsIdentity))
}
