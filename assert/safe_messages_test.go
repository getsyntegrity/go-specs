package assert

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// Pinned before the matcher diagnostics were routed through the safe renderer: an ordinary value
// passes the safety pre-check and is printed by fmt exactly as it always was.
var ordinaryMessages = map[string]string{
	"Equal/int":                            "expected 2 to equal 1",
	"Equal/int/description":                "equal to 1",
	"Equal/struct":                         "expected {1 3} to equal {1 2}\ndifferences:\n  compatPoint.Y: expected 2, actual 3",
	"Equal/struct/description":             "equal to {1 2}",
	"Equal/map":                            "expected map[a:2] to equal map[a:1]\ndifferences:\n  [\"a\"]: expected 1, actual 2",
	"Equal/map/description":                "equal to map[a:1]",
	"Equal/error":                          "expected error b (*errors.errorString) to match a (*errors.errorString) — errors.Is(actual, expected) is false",
	"Equal/error/description":              "equal to a",
	"Equal/duration":                       "expected 2s to equal 1s",
	"Equal/duration/description":           "equal to 1s",
	"NotEqual":                             "expected [1 2] not to equal [1 2]",
	"NotEqual/description":                 "not equal to [1 2]",
	"BeNil":                                "expected nil, got &{1 2} (*assert.compatPoint)",
	"BeNil/description":                    "nil",
	"BeTrue":                               "expected true, got no (string)",
	"BeTrue/description":                   "true",
	"BeFalse":                              "expected false, got 3 (int)",
	"BeFalse/description":                  "false",
	"Contain/slice":                        "expected [1 2 3] to contain 9",
	"Contain/slice/description":            "containing 9",
	"Contain/string":                       "expected abc to contain zz",
	"Contain/string/description":           "containing zz",
	"Contain/typeMismatch":                 "expected [1] to contain x — []int actual needs an int expected value, got string",
	"Contain/typeMismatch/description":     "containing x",
	"Contain/unsupported":                  "expected map[a:1] to contain 1 — map[string]int is not a supported Contain actual (want string, slice, or array)",
	"Contain/unsupported/description":      "containing 1",
	"HaveLen":                              "expected [a] to have length 3, got length 1",
	"HaveLen/description":                  "having length 3",
	"HaveLen/noLength":                     "HaveLen: int has no length",
	"HaveLen/noLength/description":         "having length 3",
	"BeEmpty":                              "expected map[a:1] to be empty, got length 1",
	"BeEmpty/description":                  "empty",
	"HaveKey":                              "expected map[a:1] to have key b",
	"HaveKey/description":                  "having key b",
	"HaveKey/wrongType":                    "expected map[a:1] to have key 1 — map[string]int keys are string, got int",
	"HaveKey/wrongType/description":        "having key 1",
	"HaveValue":                            "expected map[a:1] to have value 9",
	"HaveValue/description":                "having value 9",
	"HavePair/missing":                     "expected map[a:1] to have key b with value 1 — key is missing",
	"HavePair/missing/description":         "having key b with value 1",
	"HavePair/other":                       "expected map[a:1] to have key a with value 2 — key has value 1",
	"HavePair/other/description":           "having key a with value 2",
	"ContainAllOf":                         "expected [1 2] to contain all of [1 5], missing [5]",
	"ContainAllOf/description":             "containing all of [1 5]",
	"ContainAnyOf":                         "expected [1 2] to contain any of [7 8]",
	"ContainAnyOf/description":             "containing any of [7 8]",
	"ContainTheSameElementsAs":             "expected [3 4] to contain the same elements as [1 2 3] — missing [1 2], unexpected [4]",
	"ContainTheSameElementsAs/description": "containing the same elements as [1 2 3]",
	"BeOneOf":                              "expected c to be one of [a b]",
	"BeOneOf/description":                  "one of [a b]",
	"BeGreaterThan":                        "expected 3 to be greater than 5",
	"BeGreaterThan/description":            "greater than 5",
	"BeBetween":                            "expected 9 to be between 1 and 3 (inclusive)",
	"BeBetween/description":                "between 1 and 3 (inclusive)",
	"BeCloseTo":                            "expected 12.25 to be within 0.5 of 10, difference is 2.25",
	"BeCloseTo/description":                "within 0.5 of 10",
	"BeZero":                               "expected {1 0} to be the zero value of assert.compatPoint",
	"BeZero/description":                   "the zero value",
	"Satisfy":                              "expected [1] to satisfy \"is even\"",
	"Satisfy/description":                  "satisfying \"is even\"",
	"MatchError":                           "expected error bang (*errors.errorString) to match boom (*errors.errorString) — errors.Is(actual, expected) is false",
	"MatchError/description":               "an error matching boom (*errors.errorString)",
	"MatchError/nonError":                  "expected an error matching boom (*errors.errorString), got 5 (int) — errors.Is needs an error actual",
	"MatchError/nonError/description":      "an error matching boom (*errors.errorString)",
	"MatchErrorAs":                         "expected plain (*errors.errorString) to unwrap to **assert.compatNamedErr — errors.As(actual, target) is false",
	"MatchErrorAs/description":             "an error assignable to **assert.compatNamedErr",
	"StartWith":                            "expected \"abc\" to start with \"x\"",
	"StartWith/description":                "starting with \"x\"",
	"MatchRegex":                           "expected \"abc\" to match regex \"^z\"",
	"MatchRegex/description":               "matching regex \"^z\"",
	"Not(Equal)":                           "expected 1 not to be equal to 1",
	"Not(Equal)/description":               "not equal to 1",
	"Not(HaveLen)":                         "expected [1] not to be having length 1",
	"Not(HaveLen)/description":             "not having length 1",
	"All":                                  "All: #2: \"HaveLen: int has no length\"",
	"All/description":                      "all of [equal to 1, having length 2]",
	"Any":                                  "Any: #1: \"expected 2 to equal 1\"; #2: \"expected 2 to contain x — int is not a supported Contain actual (want string, slice, or array)\"",
	"Any/description":                      "any of [equal to 1, containing x]",
	"Not(Any(Equal, BeOneOf))":             "expected 3 not to be any of [equal to 3, one of [3 4]]",
	"Not(Any(Equal, BeOneOf))/description": "not any of [equal to 3, one of [3 4]]",
}

func TestOrdinaryMatcherMessagesKeepTheirText(t *testing.T) {
	seen := 0
	for _, c := range compatCases() {
		_, msg := Evaluate(c.m, c.actual)
		if want, ok := ordinaryMessages[c.name]; !ok || msg != want {
			t.Errorf("%s:\n got  %q\n want %q", c.name, msg, want)
		}
		seen++
		if d, ok := c.m.(Describer); ok {
			seen++
			if want := ordinaryMessages[c.name+"/description"]; d.Description() != want {
				t.Errorf("%s description:\n got  %q\n want %q", c.name, d.Description(), want)
			}
		}
	}
	if seen != len(ordinaryMessages) {
		t.Errorf("checked %d messages, table pins %d", seen, len(ordinaryMessages))
	}
}

// cycErr is an error whose value reaches itself, with an Error method a safe fallback must not call.
type cycErr struct{ self *cycErr }

var cycErrCalls int

func (e *cycErr) Error() string { cycErrCalls++; return "cyc" }

// cycStringer is the same for String.
type cycStringer struct{ self *cycStringer }

func (s *cycStringer) String() string { cycErrCalls++; return "cyc-stringer" }

// cyclicValue builds a value of the named kind that fmt cannot print: it contains itself.
func cyclicValue(kind string) any {
	switch kind {
	case "map":
		m := map[string]any{}
		m["self"] = m
		return m
	case "slice":
		s := make([]any, 1)
		s[0] = s
		return s
	case "pointer":
		type node struct {
			Next *node
			Tag  string
		}
		n := &node{Tag: "n"}
		n.Next = n
		return n
	case "view":
		backing := make([]any, 2)
		long := backing[:2]
		backing[0], backing[1] = 0, long
		return long
	}
	panic("unknown kind " + kind)
}

type cyclicCase struct {
	name   string
	m      Matcher
	actual any
}

// cyclicCases builds one matcher per rendering family with the cyclic value c as actual and as every
// operand the matcher prints.
func cyclicCases(c any) []cyclicCase {
	e := &cycErr{}
	e.self = e
	e2 := &cycErr{}
	e2.self = e2
	families := []cyclicCase{
		{"Equal", Equal(c), c},
		{"Equal/other", Equal([]any{c}), c},
		{"NotEqual", NotEqual(c), c},
		{"BeNil", BeNil(), c},
		{"BeTrue", BeTrue(), c},
		{"BeFalse", BeFalse(), c},
		{"Contain", Contain(c), c},
		{"Contain/element", Contain(1), c},
		{"HaveLen", HaveLen(7), c},
		{"BeEmpty", BeEmpty(), c},
		{"HaveKey", HaveKey(c), c},
		{"HaveKey/string", HaveKey("absent"), c},
		{"HaveValue", HaveValue(c), c},
		{"HavePair", HavePair(c, c), c},
		{"HavePair/string", HavePair("absent", c), c},
		{"ContainAllOf", ContainAllOf(c, c), c},
		{"ContainAnyOf", ContainAnyOf(c, c), c},
		{"ContainTheSameElementsAs", ContainTheSameElementsAs(c), c},
		{"ContainTheSameElementsAs/list", ContainTheSameElementsAs([]any{c}), []any{1}},
		{"BeOneOf", BeOneOf(c, c), c},
		{"BeGreaterThan", BeGreaterThan(c), c},
		{"BeBetween", BeBetween(c, c), c},
		{"BeCloseTo", BeCloseTo(1, 2), c},
		{"BeZero", BeZero(), c},
		{"Satisfy", Satisfy("never", func(any) bool { return false }), c},
		{"MatchError", MatchError(e), e2},
		{"MatchError/nonError", MatchError(e), c},
		{"MatchErrorAs", MatchErrorAs(c), e},
		{"MatchErrorAs/error", MatchErrorAs(new(*cycErr)), c},
		{"Equal/errors", Equal(e), e2},
		{"NotEqual/errors", NotEqual(e), e2},
	}
	var out []cyclicCase
	for _, f := range families {
		out = append(out, f,
			cyclicCase{"Not(" + f.name + ")", Not(f.m), f.actual},
			cyclicCase{"All(" + f.name + ")", All(f.m, Equal(1)), f.actual},
			cyclicCase{"Any(" + f.name + ")", Any(f.m, Equal(1)), f.actual},
			cyclicCase{"Not(Any(" + f.name + "))", Not(Any(f.m, BeNil())), f.actual},
		)
	}
	return out
}

const safeMessagesEnv = "GO_SPECS_SAFE_MESSAGES_CHILD"

// runCyclicCases renders the failure message and the description of every case, and the poll results
// for a cyclic observation, panic and matcher operand.
func runCyclicCases(kind string) {
	c := cyclicValue(kind)
	for _, tc := range cyclicCases(c) {
		fmt.Println("CASE", tc.name)
		fmt.Println(tc.m.FailureMessage(tc.actual))
		if d, ok := tc.m.(Describer); ok {
			fmt.Println(d.Description())
		}
		_, failure := Evaluate(tc.m, tc.actual)
		fmt.Println(failure)
	}
	fmt.Println("CASE poll")
	clock := NewManualClock()
	opts := []PollOption{WithClock(clock), WithTimeout(tick), WithInterval(tick)}
	fmt.Println(Eventually(func() any { clock.Advance(tick); return c }, HaveLen(1), opts...).Message())
	fmt.Println(Eventually(func() any { clock.Advance(tick); return c }, Equal(c), opts...).Message())
	fmt.Println(Consistently(func() any { clock.Advance(tick); return c }, Not(Equal(c)), opts...).Message())
	fmt.Println(Eventually(func() any { clock.Advance(tick); panic(c) }, BeNil(), opts...).Message())
	fmt.Println("MSG-END")
}

func TestMatcherMessagesTerminateOnCyclicValues(t *testing.T) {
	if kind := os.Getenv(safeMessagesEnv); kind != "" {
		runCyclicCases(kind)
		return
	}
	for _, kind := range []string{"map", "slice", "pointer", "view"} {
		t.Run(kind, func(t *testing.T) {
			cmd := exec.Command(os.Args[0], "-test.run=^TestMatcherMessagesTerminateOnCyclicValues$")
			cmd.Env = append(os.Environ(), safeMessagesEnv+"="+kind)
			out, err := cmd.CombinedOutput()
			text := string(out)
			if err != nil || !strings.Contains(text, "MSG-END") {
				lines := strings.Split(text, "\n")
				last := ""
				for _, l := range lines {
					if strings.HasPrefix(l, "CASE ") {
						last = l
					}
				}
				head := text
				if len(head) > 500 {
					head = head[:500]
				}
				t.Fatalf("child did not finish (%v); last case started: %q\n%s", err, last, head)
			}
		})
	}
}

func TestMatcherFallbackNeverCallsUserMethods(t *testing.T) {
	cycErrCalls = 0
	s := &cycStringer{}
	s.self = s
	e := &cycErr{}
	e.self = e
	for _, m := range []Matcher{Equal(s), NotEqual(s), BeNil(), HaveLen(1), HaveKey(s), ContainAllOf(s), BeOneOf(s), MatchError(e), Not(Equal(s)), All(Equal(s))} {
		_ = m.FailureMessage(s)
		_ = m.FailureMessage(e)
		if d, ok := m.(Describer); ok {
			_ = d.Description()
		}
	}
	if cycErrCalls != 0 {
		t.Fatalf("a cyclic value's String or Error method was called %d times", cycErrCalls)
	}
}

func TestOrdinaryValuesKeepTheirStringAndErrorText(t *testing.T) {
	if got := formatUserValue(&compatNamedErr{4}, "%v"); got != "named-4" {
		t.Fatalf("an ordinary error keeps its Error text, got %q", got)
	}
	if got := formatUserValue(time.Second, "%v"); got != "1s" {
		t.Fatalf("a Duration keeps its String text, got %q", got)
	}
	if got := formatUserValue(nil, "%v"); got != "<nil>" {
		t.Fatalf("nil renders as fmt does, got %q", got)
	}
	if got := formatUserValue(map[string]int{"a": 1}, "%#v"); got != `map[string]int{"a":1}` {
		t.Fatalf("the verb is honoured on the ordinary path, got %q", got)
	}
}

// A huge acyclic value used to be printed whole by every matcher message.
func TestMatcherMessagesBoundHugeAcyclicValues(t *testing.T) {
	rows := make([][]int, 400)
	for i := range rows {
		rows[i] = make([]int, 400)
	}
	huge := any(rows)
	start := time.Now()
	for name, m := range map[string]Matcher{"HaveLen": HaveLen(1), "BeNil": BeNil(), "Contain": Contain(1), "Not": Not(HaveLen(400)),
		"BeOneOf": BeOneOf(huge), "Equal": Equal(huge), "HaveKey": HaveKey(huge)} {
		msg := m.FailureMessage(huge)
		if len(msg) > 100_000 {
			t.Errorf("%s: message is %d bytes, want it bounded", name, len(msg))
		}
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("rendering took %v", d)
	}
}
