package specs

import "testing"

// Configuration semantics — what counts as unconfigured, valid or unusable, and how each is
// reported — live in sharding_config_test.go. This file covers the partition behaviour itself.

func TestShardSpecs(t *testing.T) {
	specs := make([]RunSpec, 10)
	for i := range specs {
		specs[i] = RunSpec{Name: "spec", Fn: func(*Context) {}}
	}

	// total 1 → all specs
	out := ShardSpecs(specs, 0, 1)
	if len(out) != 10 {
		t.Errorf("shard 0/1: got %d specs, want 10", len(out))
	}

	// total 10 → one spec per shard
	for shard := 0; shard < 10; shard++ {
		out := ShardSpecs(specs, shard, 10)
		if len(out) != 1 {
			t.Errorf("shard %d/10: got %d specs, want 1", shard, len(out))
		}
	}

	// total 3 → 4, 3, 3
	for shard := 0; shard < 3; shard++ {
		out := ShardSpecs(specs, shard, 3)
		want := 4
		if shard > 0 {
			want = 3
		}
		if len(out) != want {
			t.Errorf("shard %d/3: got %d specs, want %d", shard, len(out), want)
		}
	}

	// more shards than specs: the surplus shards are legitimately empty, not an error
	if out := ShardSpecs(specs, 11, 13); len(out) != 0 {
		t.Errorf("shard 11/13 over 10 specs: got %d specs, want 0", len(out))
	}
}

func TestShardBCProgram(t *testing.T) {
	b := NewBCBuilder(32)
	b.AddBefore(func(*Context) {})
	b.AddSpec(func(*Context) {})
	b.AddSpec(func(*Context) {})
	b.AddSpec(func(*Context) {})
	prog := b.BuildBC()
	if prog.NumSpecs() != 3 {
		t.Fatalf("build: got %d specs", prog.NumSpecs())
	}

	// shard 1/3 → one spec
	shard := ShardBCProgram(prog, 1, 3)
	if shard.NumSpecs() != 1 {
		t.Errorf("shard 1/3: got %d specs, want 1", shard.NumSpecs())
	}
	if shard.BCLen() == 0 {
		t.Error("shard 1/3: code empty")
	}

	// total 1 → the whole program
	if all := ShardBCProgram(prog, 0, 1); all.NumSpecs() != 3 {
		t.Errorf("shard 0/1: got %d specs, want 3", all.NumSpecs())
	}
}

func TestFormatShardFlag(t *testing.T) {
	if s := FormatShardFlag(2, 10); s != "-shard 2/10" {
		t.Errorf("FormatShardFlag(2,10): got %q", s)
	}
}
