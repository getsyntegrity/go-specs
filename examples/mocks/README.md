# Interface mocking with mock.Controller

In-memory doubles for two ports of a small onboarding service: a `UserRepository` and an HTTP-client
abstraction (`Do(*http.Request) (*http.Response, error)`). No database, no socket: responses are built
with `httptest.NewRecorder`, never a server.

```bash
go test ./examples/mocks/... -v
```

- `onboarding.go` — the code under test and the two interfaces.
- `interface_mocks_test.go` — the hand-written typed adapters (`userRepoMock`, `httpClientMock`) and the specs.

What the specs show: stubbing with `Return`, sequential responses, `Do`, errors, counts (`Times`,
`AtLeast`, `Never`), a predicate matcher (`mock.MatchT`), a `mock.Captor`, `InOrder` across the two
adapters, and automatic verification: `mock.NewController(ctx)` registers `Verify` with `ctx.Cleanup`,
so no spec calls `Verify`. It works on every engine, `ItParallel` included.

`mocks_test.go` keeps the original `Spy` example. See `docs/DSL.md`, "Mocking interfaces with mock.Controller".
