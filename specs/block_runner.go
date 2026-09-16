// block_runner.go compiles specs into blocks and runs them with fewer outer-loop iterations.
// Each block runs multiple specs sequentially; the runner does one outer iteration per block
// instead of per spec, reducing loop overhead. No allocations during execution.
package specs

import (
	"runtime/debug"
	"testing"
)

// DefaultBlockSize is the default number of specs per block when blockSize <= 0.
const DefaultBlockSize = 8

// specBlock describes a contiguous range of spec funcs to run: fns[start:start+count].
// The runner holds the shared fns slice; blocks are index ranges only (no per-block allocation).
type specBlock struct {
	start int
	count int
}

// CompileBlocks compiles specs into blocks of size blockSize. Returns a flat slice of Fn
// (so all spec funcs are in one place) and block descriptors (start, count) for each block.
// If blockSize <= 0, DefaultBlockSize is used. Allocation happens here, not during Run.
// Execution order is deterministic: block 0 runs specs 0..blockSize-1, block 1 runs specs blockSize.., etc.
func CompileBlocks(specs []RunSpec, blockSize int) (fns []func(*Context), blocks []specBlock) {
	n := len(specs)
	if n == 0 {
		return nil, nil
	}
	if blockSize <= 0 {
		blockSize = DefaultBlockSize
	}
	fns = make([]func(*Context), n)
	for i := range specs {
		fns[i] = specs[i].Fn
	}
	numBlocks := (n + blockSize - 1) / blockSize
	blocks = make([]specBlock, 0, numBlocks)
	for i := 0; i < n; i += blockSize {
		count := min(blockSize, n-i)
		blocks = append(blocks, specBlock{start: i, count: count})
	}
	return fns, blocks
}

// BlockRunner runs specs via precompiled blocks. Created from CompileBlocks; Run does not allocate.
type BlockRunner struct {
	fns    []func(*Context)
	blocks []specBlock
}

// NewBlockRunner creates a runner that executes the given blocks. fns and blocks must be
// the slices returned by CompileBlocks; they are not copied. Do not modify them after creation.
func NewBlockRunner(fns []func(*Context), blocks []specBlock) *BlockRunner {
	return &BlockRunner{fns: fns, blocks: blocks}
}

// Run executes all blocks in order; each block runs its specs sequentially. One context
// from the pool, reused for every spec. No allocations in the loop.
//
// A panic in a spec is recovered instead of crashing the process: it's recorded as a failure
// (via ctx.recordFailure + ctx.backend.Errorf, message and stack trace), and the next spec in the
// block, and the next block, still run.
func (r *BlockRunner) Run(tb testing.TB) {
	if r == nil || tb == nil || len(r.blocks) == 0 {
		return
	}
	backend := asTestBackend(tb)
	defer putTestBackend(backend)
	ctx, release := acquireContext(backend)
	defer release()
	ctx.SetPathValues(PathValues{})

	runBlocks(ctx, r.fns, r.blocks)
}

// runBlocks runs every block's specs sequentially against ctx, recovering each spec individually.
// Split out from Run so it can be tested without a real testing.TB.
func runBlocks(ctx *Context, fns []func(*Context), blocks []specBlock) {
	for bi := 0; bi < len(blocks); bi++ {
		b := blocks[bi]
		blockFns := fns[b.start : b.start+b.count]
		n := len(blockFns)
		for i := 0; i < n; i++ {
			runBlockSpecRecovered(ctx, blockFns[i])
		}
	}
}

// runBlockSpecRecovered runs a single spec, recovering a panic so it fails just this spec instead
// of crashing the process. isExpectedAbort sentinels (a controlled backend's FailNow) are already
// recorded by the backend and must not be reported a second time.
func runBlockSpecRecovered(ctx *Context, fn func(*Context)) {
	defer func() {
		if recovered := recover(); recovered != nil && !isExpectedAbort(recovered) {
			ctx.recordFailure()
			ctx.backend.Errorf("panic: %v\n%s", recovered, debug.Stack())
		}
	}()
	fn(ctx)
}

// NumSpecs returns the total number of specs (sum of all block counts).
func (r *BlockRunner) NumSpecs() int {
	if r == nil {
		return 0
	}
	return len(r.fns)
}

// NumBlocks returns the number of blocks.
func (r *BlockRunner) NumBlocks() int {
	if r == nil {
		return 0
	}
	return len(r.blocks)
}
