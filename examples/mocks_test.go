// mocks_test.go shows mock.Controller, the expectation-based test double of go-specs.
//
// Use a controller when a dependency must return scripted values and the test must also check how
// the dependency was used: which arguments, how many times, in which order. If you only need to
// record calls, a plain spy is enough (spies_test.go).
//
// The model in one paragraph. There is no code generation. You write a small adapter struct that
// implements your interface by forwarding each method to ctrl.Method("Name").Call(args...) and
// turning the returned mock.Result back into typed values (see mocksUserRepo below). In the test
// you declare what should happen with ctrl.Method("Name").Expect(argMatchers...) plus a count
// (Times, AtLeast, AtMost, AnyTimes, Never) and a response (Return, Do). NewController registers
// Verify with the test's Cleanup, so unmet expectations and InOrder violations are reported when
// the case ends, even when its body failed. An unexpected or forbidden call is reported
// immediately through Errorf, and the code under test keeps running with a zero Result.
//
// Semantics worth knowing:
//   - Expect without a count means exactly once (Times(1)).
//   - Plain values in Expect are wrapped in mock.Equal; mock.Any, mock.Match, mock.MatchT and a
//     Captor's Matcher() are the other argument matchers.
//   - Among expectations that match a call, prohibitions (Never) win, then the first one with
//     capacity is used.
//   - Return can be repeated to build a sequence; when it runs out, the last response repeats.
//     Do computes the result from the arguments and wins over Return.
//   - Result does not know your signature: Get(i) reads any value, Err(i) reads an error, and
//     mock.Value[T](r, i) reads a typed value. An unconfigured index reads as nil or the zero T.
//   - Recorded arguments are the caller's values, not deep copies.
//   - A controller is safe for concurrent use, so it works with ItParallel.
//
// The Examples below use a small fake test (mocksTB) so that failure messages can be printed
// without failing anything. In a real spec pass the *specs.Context instead: it satisfies mock.TB.
package examples_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/getsyntegrity/go-specs/mock"
	"github.com/getsyntegrity/go-specs/specs"
)

// mocksTB is a minimal mock.TB. It records Errorf messages and keeps Cleanup functions so the
// example can decide when "the test ends" (finish), exactly like a real test runner would.
type mocksTB struct {
	errors   []string
	cleanups []func()
}

func (f *mocksTB) Helper()           {}
func (f *mocksTB) Cleanup(fn func()) { f.cleanups = append(f.cleanups, fn) }
func (f *mocksTB) Errorf(format string, args ...any) {
	f.errors = append(f.errors, fmt.Sprintf(format, args...))
}

// finish runs the registered cleanups in reverse order, as testing.T does.
func (f *mocksTB) finish() {
	for i := len(f.cleanups) - 1; i >= 0; i-- {
		f.cleanups[i]()
	}
}

var mocksSiteRE = regexp.MustCompile(`declared at [^\s:)]+:\d+`)

// print writes every recorded message. The declaration site (file:line of the Expect call) is
// masked so the output does not depend on line numbers.
func (f *mocksTB) print() {
	if len(f.errors) == 0 {
		fmt.Println("no failures")
	}
	for _, e := range f.errors {
		fmt.Println(mocksSiteRE.ReplaceAllString(e, "declared at <site>"))
	}
}

// The basics: Expect declares the call, Return scripts the answer, Call is what an adapter
// invokes. The adapter reads the answer back by index.
func Example_mocksExpectAndReturn() {
	tb := &mocksTB{}
	ctrl := mock.NewController(tb)
	ctrl.Method("Find").Expect("u1").Return("Ada", nil)

	r := ctrl.Method("Find").Call("u1")

	fmt.Println(r.Get(0), r.Err(1))
	fmt.Println(mock.Value[string](r, 0))
	tb.finish() // Verify runs here: the expectation was met
	tb.print()
	// Output:
	// Ada <nil>
	// Ada
	// no failures
}

// Result accessors never guess. An index that was not configured reads as nil (Get), no error (Err)
// or the zero value (Value). A configured value of the wrong type is a programming error in the
// adapter and panics with a message naming the method and index.
func Example_mocksResultAccessors() {
	ctrl := mock.NewController(&mocksTB{})
	ctrl.Method("Load").Expect().Return(42)

	r := ctrl.Method("Load").Call()

	fmt.Println(r.Get(0), r.Get(5), r.Err(1))
	fmt.Println(mock.Value[int](r, 0), mock.Value[string](r, 3) == "")

	func() {
		defer func() { fmt.Println("panic:", recover()) }()
		_ = mock.Value[string](r, 0) // 42 is not a string
	}()
	func() {
		defer func() { fmt.Println("panic:", recover()) }()
		_ = r.Err(0) // 42 is not an error
	}()
	// Output:
	// 42 <nil> <nil>
	// 42 true
	// panic: mock: Load result 0 is int, want string
	// panic: mock: Load result 0 is int, which is not an error
}

// Count modifiers. The default is exactly once. Times(n) is exactly n, AtLeast and AtMost set one
// bound (and combine into a range), AnyTimes accepts zero or more, Never forbids the call.
func Example_mocksCallCounts() {
	tb := &mocksTB{}
	ctrl := mock.NewController(tb)
	ctrl.Method("Once").Expect(mock.Any())                       // exactly 1
	ctrl.Method("Twice").Expect(mock.Any()).Times(2)             // exactly 2
	ctrl.Method("Range").Expect(mock.Any()).AtLeast(1).AtMost(3) // 1 to 3
	ctrl.Method("Free").Expect(mock.Any()).AnyTimes()            // 0 or more

	ctrl.Method("Once").Call("a")
	ctrl.Method("Twice").Call("a")
	ctrl.Method("Twice").Call("b")
	ctrl.Method("Range").Call("a")
	ctrl.Method("Range").Call("b")

	tb.finish()
	tb.print()
	// Output: no failures
}

// A call beyond the allowed count is an unexpected call. It is reported immediately, with the
// reason each expectation rejected it, and the code under test keeps running.
func Example_mocksTooManyCallsDiagnostic() {
	tb := &mocksTB{}
	ctrl := mock.NewController(tb)
	ctrl.Method("Charge").Expect(mock.Any()).Return(nil) // exactly once

	ctrl.Method("Charge").Call(10)
	ctrl.Method("Charge").Call(20)

	tb.print()
	// Output:
	// mock: unexpected call Charge(20):
	//   expectation 1 Charge(any value) declared at <site>: matched but at capacity (want 1, got 1)
}

// An expectation that was never satisfied is reported by Verify, which NewController registers
// with the test's Cleanup. The message names the expectation, where it was declared and the count.
func Example_mocksUnmetExpectationDiagnostic() {
	tb := &mocksTB{}
	ctrl := mock.NewController(tb)
	ctrl.Method("Charge").Expect(mock.Equal(10)).Times(2)

	ctrl.Method("Charge").Call(10)
	tb.finish()

	tb.print()
	// Output: mock: unmet expectation Charge(equal to 10) declared at <site>: want 2, got 1
}

// A call whose arguments match no expectation lists why each one was rejected, so a wrong
// argument is easy to spot.
func Example_mocksUnexpectedArgumentsDiagnostic() {
	tb := &mocksTB{}
	ctrl := mock.NewController(tb)
	ctrl.Method("Send").Expect("a@example.com", mock.MatchT("a positive size", func(n int) bool { return n > 0 })).AnyTimes()

	ctrl.Method("Send").Call("b@example.com", 5)
	ctrl.Method("Send").Call("a@example.com", -1)

	tb.print()
	// Output:
	// mock: unexpected call Send("b@example.com", 5):
	//   expectation 1 Send(equal to "a@example.com", a positive size (of type int)) declared at <site>: argument 0: got "b@example.com", want equal to "a@example.com"
	// mock: unexpected call Send("a@example.com", -1):
	//   expectation 1 Send(equal to "a@example.com", a positive size (of type int)) declared at <site>: argument 1: got -1, want a positive size (of type int)
}

// Never marks a call as forbidden. It wins over any permissive expectation declared for the same
// arguments, so a broad AnyTimes cannot swallow it. The violation is reported the moment it happens.
func Example_mocksNeverIsForbidden() {
	tb := &mocksTB{}
	ctrl := mock.NewController(tb)
	ctrl.Method("Delete").Expect(mock.Any()).AnyTimes() // permissive
	ctrl.Method("Delete").Expect("prod").Never()        // but never "prod"

	ctrl.Method("Delete").Call("staging")
	ctrl.Method("Delete").Call("prod")

	tb.print()
	// Output: mock: forbidden call Delete("prod"): expectation Delete(equal to "prod") declared at <site> says never
}

// Return called several times builds a sequence: the n-th matching call gets the n-th response.
// When the sequence is shorter than the allowed count, the last response repeats.
func Example_mocksSequentialReturns() {
	ctrl := mock.NewController(&mocksTB{})
	ctrl.Method("Poll").Expect().Times(3).
		Return("pending").
		Return("running")

	for range 3 {
		fmt.Println(ctrl.Method("Poll").Call().Get(0))
	}
	// Output:
	// pending
	// running
	// running
}

// Do computes the answer from the actual arguments. It receives a copy of them, returns the result
// values, and wins over Return when both are set. It runs outside the controller lock, so it may
// call other mocked methods of the same controller.
func Example_mocksDoComputesResults() {
	ctrl := mock.NewController(&mocksTB{})
	ctrl.Method("Add").Expect(mock.Any(), mock.Any()).AnyTimes().Do(func(args []any) []any {
		return []any{args[0].(int) + args[1].(int)}
	})

	fmt.Println(ctrl.Method("Add").Call(2, 3).Get(0))
	fmt.Println(ctrl.Method("Add").Call(10, 5).Get(0))
	// Output:
	// 5
	// 15
}

// Argument matchers. A plain value means Equal. Any accepts everything. Match takes a predicate
// over any, MatchT a typed one (a value of another type does not match). The description you give
// appears in diagnostics, so write it as what is wanted.
func Example_mocksArgumentMatchers() {
	tb := &mocksTB{}
	ctrl := mock.NewController(tb)
	ctrl.Method("Book").Expect(
		"room-1", // Equal
		mock.Any(),
		mock.Match("a weekday", func(v any) bool { d, ok := v.(string); return ok && d != "sat" && d != "sun" }),
		mock.MatchT("between 1 and 8 guests", func(n int) bool { return n >= 1 && n <= 8 }),
	).Return(true)

	ok := mock.Value[bool](ctrl.Method("Book").Call("room-1", "any note", "tue", 4), 0)
	fmt.Println(ok)

	ctrl.Method("Book").Call("room-1", "any note", "sun", 4)
	tb.print()
	// Output:
	// true
	// mock: unexpected call Book("room-1", "any note", "sun", 4):
	//   expectation 1 Book(equal to "room-1", any value, a weekday, between 1 and 8 guests (of type int)) declared at <site>: argument 2: got "sun", want a weekday
}

// A Captor records the values the code under test actually passed, so you can assert on them after
// the fact. It records only values of calls that the expectation claimed, so a rejected call
// leaves it untouched.
func Example_mocksCaptor() {
	ctrl := mock.NewController(&mocksTB{})
	subjects := mock.NewCaptor[string]()
	ctrl.Method("Send").Expect(subjects.Matcher()).Times(2)

	ctrl.Method("Send").Call("welcome")
	ctrl.Method("Send").Call("reminder")

	fmt.Println(subjects.Values(), subjects.Last())
	// Output: [welcome reminder] reminder
}

// InOrder declares that the first matching call of each expectation happens in the given order,
// across methods and controller spies. A violation is reported by Verify with the observed
// sequence.
func Example_mocksInOrder() {
	tb := &mocksTB{}
	ctrl := mock.NewController(tb)
	open := ctrl.Method("Open").Expect().Return()
	write := ctrl.Method("Write").Expect("data").Return()
	closeFile := ctrl.Method("Close").Expect().Return()
	ctrl.InOrder(open, write, closeFile)

	ctrl.Method("Open").Call()
	ctrl.Method("Close").Call() // too early
	ctrl.Method("Write").Call("data")
	tb.finish()

	tb.print()
	// Output: mock: order violation: expected Write(equal to "data") (declared at <site>) before Close() (declared at <site>), observed sequence: #1 Open(), #2 Close(), #3 Write("data")
}

// Calls returns the recorded calls of one method; Controller.Calls returns the global log with a
// dense, 1-based sequence number across every method and controller spy. Both return copies.
func Example_mocksRecordedCalls() {
	ctrl := mock.NewController(&mocksTB{})
	ctrl.Method("A").Expect(mock.Any()).AnyTimes()
	ctrl.Method("B").Expect(mock.Any()).AnyTimes()

	ctrl.Method("A").Call(1)
	ctrl.Method("B").Call("x")
	ctrl.Method("A").Call(2)

	for _, c := range ctrl.Calls() {
		fmt.Println(c.Seq, c.Method, c.Args)
	}
	fmt.Println(len(ctrl.Method("A").Calls()), len(ctrl.Method("B").Calls()))
	// Output:
	// 1 A [1]
	// 2 B [x]
	// 3 A [2]
	// 2 1
}

// Reset drops expectations, recorded calls and order constraints and re-arms Verify, so one
// controller can be reused for a second phase of a test.
func Example_mocksReset() {
	tb := &mocksTB{}
	ctrl := mock.NewController(tb)
	ctrl.Method("Ping").Expect().Return()
	ctrl.Method("Ping").Call()

	ctrl.Reset()
	fmt.Println(len(ctrl.Calls()))

	ctrl.Method("Ping").Expect().Times(2) // a fresh expectation, not met on purpose
	ctrl.Method("Ping").Call()
	tb.finish()
	tb.print()
	// Output:
	// 0
	// mock: unmet expectation Ping() declared at <site>: want 2, got 1
}

// Verify can also be called explicitly to check earlier than the end of the test. It is idempotent:
// only the first call after creation (or Reset) reports, so the automatic call at Cleanup does not
// repeat the same message.
func Example_mocksVerifyIsIdempotent() {
	tb := &mocksTB{}
	ctrl := mock.NewController(tb)
	ctrl.Method("Save").Expect(mock.Any())

	ctrl.Verify()
	ctrl.Verify()
	tb.finish()

	fmt.Println(len(tb.errors))
	// Output: 1
}

// --- The domain used by the interface-mocking specs --------------------------------------------
//
// A small onboarding service depends on a repository interface and an HTTP-client abstraction.
// Nothing here talks to a database or a socket: the specs below replace both dependencies with
// typed adapters backed by one mock.Controller.

type mocksUser struct {
	ID     string
	Name   string
	Avatar string
}

var (
	errMocksNotFound = errors.New("user not found")
	errMocksExists   = errors.New("user already exists")
)

// mocksUserRepository is the persistence port. A real implementation would use a database.
type mocksUserRepository interface {
	Find(ctx context.Context, id string) (*mocksUser, error)
	Save(ctx context.Context, u *mocksUser) error
}

// mocksHTTPClient is the HTTP abstraction the service depends on; *http.Client satisfies it.
type mocksHTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// mocksOnboarding registers users and enriches them with an avatar from a profile service.
type mocksOnboarding struct {
	Repo    mocksUserRepository
	HTTP    mocksHTTPClient
	BaseURL string
}

// Register creates the user id. It fails with errMocksExists when the id is taken. The avatar comes
// from GET {BaseURL}/api/users/{id}: a transport error is retried once, and a non-200 answer leaves
// the avatar empty. Two transport errors in a row abort the registration before anything is saved.
func (o mocksOnboarding) Register(ctx context.Context, id, name string) (*mocksUser, error) {
	existing, err := o.Repo.Find(ctx, id)
	switch {
	case err == nil && existing != nil:
		return nil, errMocksExists
	case err != nil && !errors.Is(err, errMocksNotFound):
		return nil, fmt.Errorf("find %s: %w", id, err)
	}
	avatar, err := o.fetchAvatar(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("fetch profile of %s: %w", id, err)
	}
	u := &mocksUser{ID: id, Name: name, Avatar: avatar}
	if err := o.Repo.Save(ctx, u); err != nil {
		return nil, fmt.Errorf("save %s: %w", id, err)
	}
	return u, nil
}

func (o mocksOnboarding) fetchAvatar(ctx context.Context, id string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, o.BaseURL+"/api/users/"+id, nil)
	if err != nil {
		return "", err
	}
	var lastErr error
	for range 2 {
		resp, err := o.HTTP.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		return mocksReadAvatar(resp)
	}
	return "", lastErr
}

func mocksReadAvatar(resp *http.Response) (string, error) {
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", nil
	}
	var body struct {
		Avatar string `json:"avatar"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", err
	}
	return body.Avatar, nil
}

// --- Hand-written typed doubles ----------------------------------------------------------------
//
// These adapters are the documented way to mock an interface with go-specs: a small struct that
// implements the interface by forwarding every call to Controller.Method(name).Call and turning
// the stubbed Result back into typed return values.

// mocksUserRepo implements mocksUserRepository.
type mocksUserRepo struct{ c *mock.Controller }

func (m mocksUserRepo) Find(ctx context.Context, id string) (*mocksUser, error) {
	r := m.c.Method("UserRepository.Find").Call(ctx, id)
	return mock.Value[*mocksUser](r, 0), r.Err(1)
}

func (m mocksUserRepo) Save(ctx context.Context, u *mocksUser) error {
	r := m.c.Method("UserRepository.Save").Call(ctx, u)
	return r.Err(0)
}

// mocksHTTPClientDouble implements mocksHTTPClient.
type mocksHTTPClientDouble struct{ c *mock.Controller }

func (m mocksHTTPClientDouble) Do(req *http.Request) (*http.Response, error) {
	r := m.c.Method("HTTPClient.Do").Call(req)
	return mock.Value[*http.Response](r, 0), r.Err(1)
}

// mocksJSONResponse builds a response entirely in memory: no server, no socket.
func mocksJSONResponse(status int, body string) *http.Response {
	rec := httptest.NewRecorder()
	rec.Header().Set("Content-Type", "application/json")
	rec.WriteHeader(status)
	_, _ = rec.WriteString(body)
	return rec.Result()
}

// mocksFixture wires the service under test to both doubles. One controller means one global call
// order across the two adapters. NewController registers Verify with ctx.Cleanup, so every
// expectation is checked automatically when the case ends, even when the body fails: no spec below
// calls Verify.
type mocksFixture struct {
	ctrl *mock.Controller
	svc  mocksOnboarding
}

func newMocksFixture(ctx *specs.Context) mocksFixture {
	ctrl := mock.NewController(ctx)
	return mocksFixture{
		ctrl: ctrl,
		svc: mocksOnboarding{
			Repo:    mocksUserRepo{ctrl},
			HTTP:    mocksHTTPClientDouble{ctrl},
			BaseURL: "https://profiles.example",
		},
	}
}

func TestMocks_interfaceDoubles(t *testing.T) {
	bg := context.Background()

	specs.Describe(t, "Onboarding.Register", func(s *specs.Spec) {
		s.It("saves the user with the avatar the profile service returned", func(ctx *specs.Context) {
			f := newMocksFixture(ctx)
			saved := mock.NewCaptor[*mocksUser]()
			find := f.ctrl.Method("UserRepository.Find").Expect(mock.Any(), "u1").Return(nil, errMocksNotFound)
			get := f.ctrl.Method("HTTPClient.Do").
				Expect(mock.MatchT("a GET to /api/users/u1", func(r *http.Request) bool {
					return r.Method == http.MethodGet && r.URL.Path == "/api/users/u1"
				})).
				Return(mocksJSONResponse(200, `{"avatar":"u1.png"}`), nil)
			save := f.ctrl.Method("UserRepository.Save").Expect(mock.Any(), saved.Matcher()).Return(nil)
			// The repository is asked first, then the profile service, then the user is saved.
			f.ctrl.InOrder(find, get, save)

			u, err := f.svc.Register(bg, "u1", "Ada")

			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(u.Avatar).ToEqual("u1.png")
			// The captor holds what the service actually passed to Save.
			ctx.Expect(saved.Last().Name).ToEqual("Ada")
			ctx.Expect(saved.Values()).To(specs.HaveLen(1))
		})

		s.It("rejects an id that is taken without calling the profile service or saving", func(ctx *specs.Context) {
			f := newMocksFixture(ctx)
			f.ctrl.Method("UserRepository.Find").Expect(mock.Any(), "u1").Return(&mocksUser{ID: "u1"}, nil)
			// Never: a matching call is reported immediately as a forbidden call, even when a
			// permissive expectation also matches.
			f.ctrl.Method("HTTPClient.Do").Expect(mock.Any()).Never()
			f.ctrl.Method("UserRepository.Save").Expect(mock.Any(), mock.Any()).Never()

			_, err := f.svc.Register(bg, "u1", "Ada")

			ctx.Expect(errors.Is(err, errMocksExists)).To(specs.BeTrue())
		})

		s.It("retries a transport error once: sequential responses", func(ctx *specs.Context) {
			f := newMocksFixture(ctx)
			f.ctrl.Method("UserRepository.Find").Expect(mock.Any(), mock.Any()).AtLeast(1).Return(nil, errMocksNotFound)
			// The first matching call gets the first Return, the second call the second one.
			f.ctrl.Method("HTTPClient.Do").Expect(mock.Any()).Times(2).
				Return(nil, errors.New("connection reset")).
				Return(mocksJSONResponse(200, `{"avatar":"retry.png"}`), nil)
			f.ctrl.Method("UserRepository.Save").Expect(mock.Any(), mock.Any()).Return(nil)

			u, err := f.svc.Register(bg, "u2", "Grace")

			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(u.Avatar).ToEqual("retry.png")
		})

		s.It("gives up after two transport errors and saves nothing", func(ctx *specs.Context) {
			f := newMocksFixture(ctx)
			f.ctrl.Method("UserRepository.Find").Expect(mock.Any(), mock.Any()).Return(nil, errMocksNotFound)
			// One Return with Times(2): the last (here the only) response repeats.
			f.ctrl.Method("HTTPClient.Do").Expect(mock.Any()).Times(2).Return(nil, errors.New("timeout"))
			f.ctrl.Method("UserRepository.Save").Expect(mock.Any(), mock.Any()).Never()

			_, err := f.svc.Register(bg, "u3", "Linus")

			ctx.Expect(err).To(specs.Not(specs.BeNil()))
			ctx.Expect(strings.Contains(err.Error(), "timeout")).To(specs.BeTrue())
		})

		s.It("computes the answer from the request: Do", func(ctx *specs.Context) {
			f := newMocksFixture(ctx)
			f.ctrl.Method("UserRepository.Find").Expect(mock.Any(), mock.Any()).Return(nil, errMocksNotFound)
			f.ctrl.Method("HTTPClient.Do").Expect(mock.Any()).Do(func(args []any) []any {
				req := args[0].(*http.Request)
				id := req.URL.Path[strings.LastIndex(req.URL.Path, "/")+1:]
				return []any{mocksJSONResponse(200, `{"avatar":"`+id+`.png"}`), nil}
			})
			f.ctrl.Method("UserRepository.Save").Expect(mock.Any(), mock.Any()).Return(errors.New("disk full"))

			_, err := f.svc.Register(bg, "u4", "Ken")

			ctx.Expect(err).To(specs.Not(specs.BeNil()))
			calls := f.ctrl.Method("UserRepository.Save").Calls()
			ctx.Expect(calls).To(specs.HaveLen(1))
			ctx.Expect(calls[0].Args[1].(*mocksUser).Avatar).ToEqual("u4.png")
		})

		// Every engine works: the controller only needs a Context, and it is safe for concurrent use.
		s.ItParallel("keeps the avatar empty on a non-200 answer (parallel case)", func(ctx *specs.Context) {
			f := newMocksFixture(ctx)
			f.ctrl.Method("UserRepository.Find").Expect(mock.Any(), mock.Any()).Return(nil, errMocksNotFound)
			f.ctrl.Method("HTTPClient.Do").Expect(mock.Any()).Return(mocksJSONResponse(404, `{}`), nil)
			f.ctrl.Method("UserRepository.Save").Expect(mock.Any(), mock.Any()).Return(nil)

			u, err := f.svc.Register(bg, "u5", "Rob")

			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(u.Avatar).ToEqual("")
		})
	})
}
