package mocks_test

import (
	"testing"

	"github.com/getsyntegrity/go-specs/specs"
	"github.com/getsyntegrity/go-specs/mock"
)

func TestMocks(t *testing.T) {
	specs.Describe(t, "email service", func(s *specs.Spec) {
		s.It("sends email", func(ctx *specs.Context) {
			m := mock.New()
			sendEmail := m.Spy("sendEmail")

			sendEmail.Call("user@test.com")

			ctx.Expect(sendEmail.CallCount()).ToEqual(1)

			if !sendEmail.CalledWith(mock.Equal("user@test.com")) {
				t.Fatalf("expected email call with user@test.com")
			}
		})
	})
}
