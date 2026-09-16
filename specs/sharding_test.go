package specs

import "testing"

func TestShardSpecs(t *testing.T) {
	specs := make([]RunSpec, 10)
	for i := range specs {
		i := i
		specs[i] = RunSpec{Name: "spec", Fn: func(*Context) {}}
		_ = i
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

	// invalid: return original
	out = ShardSpecs(specs, -1, 5)
	if len(out) != 10 {
		t.Errorf("invalid shard: want passthrough 10, got %d", len(out))
	}
	out = ShardSpecs(specs, 0, 0)
	if len(out) != 10 {
		t.Errorf("invalid total: want passthrough 10, got %d", len(out))
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

	// invalid: return original
	out := ShardBCProgram(prog, -1, 2)
	if out.NumSpecs() != 3 {
		t.Errorf("invalid: want passthrough 3 specs, got %d", out.NumSpecs())
	}
}

func TestParseShardString(t *testing.T) {
	for _, tc := range []struct {
		s   string
		sh  int
		tot int
		ok  bool
	}{
		{"2/10", 2, 10, true},
		{"0/1", 0, 1, true},
		{" 3 / 5 ", 3, 5, true},
		{"", 0, 0, false},
		{"2", 0, 0, false},
		{"2/10/1", 0, 0, false},
		{"-1/5", 0, 0, false},
		{"2/2", 0, 0, false},
		{"10/10", 0, 0, false},
	} {
		sh, tot, ok := ParseShardString(tc.s)
		if ok != tc.ok || sh != tc.sh || tot != tc.tot {
			t.Errorf("ParseShardString(%q): got (%d,%d,%v) want (%d,%d,%v)", tc.s, sh, tot, ok, tc.sh, tc.tot, tc.ok)
		}
	}
}

func TestParseShardFlag(t *testing.T) {
	shard, total, ok := ParseShardFlag([]string{"prog", "-shard", "2/10", "other"})
	if !ok || shard != 2 || total != 10 {
		t.Errorf("ParseShardFlag: got (%d,%d,%v) want (2,10,true)", shard, total, ok)
	}
	_, _, ok = ParseShardFlag([]string{"prog", "run"})
	if ok {
		t.Error("ParseShardFlag: expected false when no -shard")
	}
}

func TestParseShardEnv(t *testing.T) {
	t.Setenv("SHARD", "3/7")
	shard, total, ok := ParseShardEnv()
	if !ok || shard != 3 || total != 7 {
		t.Errorf("ParseShardEnv(SHARD=3/7): got (%d,%d,%v)", shard, total, ok)
	}

	t.Setenv("SHARD", "")
	t.Setenv("SHARD_INDEX", "1")
	t.Setenv("SHARD_TOTAL", "4")
	shard, total, ok = ParseShardEnv()
	if !ok || shard != 1 || total != 4 {
		t.Errorf("ParseShardEnv(INDEX/TOTAL): got (%d,%d,%v)", shard, total, ok)
	}
}

func TestFormatShardFlag(t *testing.T) {
	if s := FormatShardFlag(2, 10); s != "-shard 2/10" {
		t.Errorf("FormatShardFlag(2,10): got %q", s)
	}
}
