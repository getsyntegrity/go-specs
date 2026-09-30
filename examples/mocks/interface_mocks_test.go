package mocks_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/getsyntegrity/go-specs/examples/mocks"
	"github.com/getsyntegrity/go-specs/mock"
	"github.com/getsyntegrity/go-specs/specs"
)

// The adapters below are the documented way to mock an interface with go-specs: a small hand-written
// struct that implements the interface by forwarding every call to mock.Controller.Method(name).Call
// and turning the stubbed Result back into typed return values. No code generation.

// userRepoMock implements mocks.UserRepository.
type userRepoMock struct{ c *mock.Controller }

func (m userRepoMock) Find(ctx context.Context, id string) (*mocks.User, error) {
	r := m.c.Method("UserRepository.Find").Call(ctx, id)
	return mock.Value[*mocks.User](r, 0), r.Err(1)
}

func (m userRepoMock) Save(ctx context.Context, u *mocks.User) error {
	r := m.c.Method("UserRepository.Save").Call(ctx, u)
	return r.Err(0)
}

// httpClientMock implements mocks.HTTPClient.
type httpClientMock struct{ c *mock.Controller }

func (m httpClientMock) Do(req *http.Request) (*http.Response, error) {
	r := m.c.Method("HTTPClient.Do").Call(req)
	return mock.Value[*http.Response](r, 0), r.Err(1)
}

// jsonResponse builds a response entirely in memory: no server, no socket.
func jsonResponse(status int, body string) *http.Response {
	rec := httptest.NewRecorder()
	rec.Header().Set("Content-Type", "application/json")
	rec.WriteHeader(status)
	_, _ = rec.WriteString(body)
	return rec.Result()
}

// fixture wires the service under test to both mocks. One controller means one global call order
// across the two adapters. NewController registers Verify with ctx.Cleanup, so every expectation is
// checked automatically when the case ends, even when the body fails: no spec calls Verify.
type fixture struct {
	ctrl *mock.Controller
	repo userRepoMock
	http httpClientMock
	svc  mocks.Onboarding
}

func newFixture(ctx *specs.Context) fixture {
	ctrl := mock.NewController(ctx)
	f := fixture{ctrl: ctrl, repo: userRepoMock{ctrl}, http: httpClientMock{ctrl}}
	f.svc = mocks.Onboarding{Repo: f.repo, HTTP: f.http, BaseURL: "https://profiles.example"}
	return f
}

var bg = context.Background()

func TestInterfaceMocks(t *testing.T) {
	specs.Describe(t, "Onboarding.Register", func(s *specs.Spec) {
		s.It("saves the user with the avatar the profile service returned", func(ctx *specs.Context) {
			f := newFixture(ctx)
			saved := mock.NewCaptor[*mocks.User]()
			find := f.ctrl.Method("UserRepository.Find").Expect(mock.Any(), "u1").Return(nil, mocks.ErrNotFound)
			get := f.ctrl.Method("HTTPClient.Do").
				Expect(mock.MatchT("a GET to /api/users/u1", func(r *http.Request) bool {
					return r.Method == http.MethodGet && r.URL.Path == "/api/users/u1"
				})).
				Return(jsonResponse(200, `{"avatar":"u1.png"}`), nil)
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
			f := newFixture(ctx)
			f.ctrl.Method("UserRepository.Find").Expect(mock.Any(), "u1").Return(&mocks.User{ID: "u1"}, nil)
			// Never: a matching call is reported immediately as a forbidden call, even when a permissive expectation also matches.
			f.ctrl.Method("HTTPClient.Do").Expect(mock.Any()).Never()
			f.ctrl.Method("UserRepository.Save").Expect(mock.Any(), mock.Any()).Never()

			_, err := f.svc.Register(bg, "u1", "Ada")

			ctx.Expect(errors.Is(err, mocks.ErrExists)).To(specs.BeTrue())
		})

		s.It("retries a transport error once: sequential responses", func(ctx *specs.Context) {
			f := newFixture(ctx)
			f.ctrl.Method("UserRepository.Find").Expect(mock.Any(), mock.Any()).AtLeast(1).Return(nil, mocks.ErrNotFound)
			// The first matching call gets the first Return, the second call the second one.
			f.ctrl.Method("HTTPClient.Do").Expect(mock.Any()).Times(2).
				Return(nil, errors.New("connection reset")).
				Return(jsonResponse(200, `{"avatar":"retry.png"}`), nil)
			f.ctrl.Method("UserRepository.Save").Expect(mock.Any(), mock.Any()).Return(nil)

			u, err := f.svc.Register(bg, "u2", "Grace")

			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(u.Avatar).ToEqual("retry.png")
		})

		s.It("gives up after two transport errors and saves nothing", func(ctx *specs.Context) {
			f := newFixture(ctx)
			f.ctrl.Method("UserRepository.Find").Expect(mock.Any(), mock.Any()).Return(nil, mocks.ErrNotFound)
			// One Return with Times(2): the last (here the only) response repeats.
			f.ctrl.Method("HTTPClient.Do").Expect(mock.Any()).Times(2).Return(nil, errors.New("timeout"))
			f.ctrl.Method("UserRepository.Save").Expect(mock.Any(), mock.Any()).Never()

			_, err := f.svc.Register(bg, "u3", "Linus")

			ctx.Expect(err).To(specs.Not(specs.BeNil()))
			ctx.Expect(strings.Contains(err.Error(), "timeout")).To(specs.BeTrue())
		})

		s.It("computes the answer from the request: Do", func(ctx *specs.Context) {
			f := newFixture(ctx)
			f.ctrl.Method("UserRepository.Find").Expect(mock.Any(), mock.Any()).Return(nil, mocks.ErrNotFound)
			f.ctrl.Method("HTTPClient.Do").Expect(mock.Any()).Do(func(args []any) []any {
				req := args[0].(*http.Request)
				id := req.URL.Path[strings.LastIndex(req.URL.Path, "/")+1:]
				return []any{jsonResponse(200, `{"avatar":"`+id+`.png"}`), nil}
			})
			f.ctrl.Method("UserRepository.Save").Expect(mock.Any(), mock.Any()).Return(errors.New("disk full"))

			_, err := f.svc.Register(bg, "u4", "Ken")

			ctx.Expect(err).To(specs.Not(specs.BeNil()))
			calls := f.ctrl.Method("UserRepository.Save").Calls()
			ctx.Expect(calls).To(specs.HaveLen(1))
			ctx.Expect(calls[0].Args[1].(*mocks.User).Avatar).ToEqual("u4.png")
		})

		// Every engine works: the controller only needs a Context.
		s.ItParallel("keeps the avatar empty on a non-200 answer (parallel case)", func(ctx *specs.Context) {
			f := newFixture(ctx)
			f.ctrl.Method("UserRepository.Find").Expect(mock.Any(), mock.Any()).Return(nil, mocks.ErrNotFound)
			f.ctrl.Method("HTTPClient.Do").Expect(mock.Any()).Return(jsonResponse(404, `{}`), nil)
			f.ctrl.Method("UserRepository.Save").Expect(mock.Any(), mock.Any()).Return(nil)

			u, err := f.svc.Register(bg, "u5", "Rob")

			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(u.Avatar).ToEqual("")
		})
	})
}
