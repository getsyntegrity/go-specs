// Package mocks is the code under test for the interface-mocking examples: a small onboarding
// service that depends on a repository interface and an HTTP-client abstraction. Nothing here talks to
// a database or a socket; the specs in interface_mocks_test.go replace both dependencies with typed
// adapters backed by mock.Controller.
package mocks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

// User is the entity the repository stores.
type User struct {
	ID     string
	Name   string
	Avatar string
}

// ErrNotFound is what a UserRepository returns when no user has the given id.
var ErrNotFound = errors.New("user not found")

// ErrExists is returned by Register when the id is already taken.
var ErrExists = errors.New("user already exists")

// UserRepository is the persistence port. A real implementation would use a database.
type UserRepository interface {
	Find(ctx context.Context, id string) (*User, error)
	Save(ctx context.Context, u *User) error
}

// HTTPClient is the HTTP abstraction the service depends on; *http.Client satisfies it.
type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// Onboarding registers users and enriches them with an avatar from a profile service.
type Onboarding struct {
	Repo    UserRepository
	HTTP    HTTPClient
	BaseURL string // profile service base URL, for example "https://profiles.example"
}

// Register creates the user id. It fails with ErrExists when the id is taken. The avatar comes from
// GET {BaseURL}/api/users/{id}: a transport error is retried once, and a non-200 answer simply leaves
// the avatar empty. Two transport errors in a row abort the registration before anything is saved.
func (o Onboarding) Register(ctx context.Context, id, name string) (*User, error) {
	existing, err := o.Repo.Find(ctx, id)
	switch {
	case err == nil && existing != nil:
		return nil, ErrExists
	case err != nil && !errors.Is(err, ErrNotFound):
		return nil, fmt.Errorf("find %s: %w", id, err)
	}
	avatar, err := o.fetchAvatar(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("fetch profile of %s: %w", id, err)
	}
	u := &User{ID: id, Name: name, Avatar: avatar}
	if err := o.Repo.Save(ctx, u); err != nil {
		return nil, fmt.Errorf("save %s: %w", id, err)
	}
	return u, nil
}

func (o Onboarding) fetchAvatar(ctx context.Context, id string) (string, error) {
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
		return readAvatar(resp)
	}
	return "", lastErr
}

func readAvatar(resp *http.Response) (string, error) {
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
