package identity

import (
	"context"
	"errors"
	"testing"
)

func TestPrincipal(t *testing.T) {
	if _, err := Require(context.Background()); !errors.Is(err, ErrUnauthenticated) {
		t.Fatal(err)
	}
	for _, p := range []Principal{{}, {UserID: "u", TeamID: "t", Role: "admin"}, {UserID: "u", Role: Intern}} {
		if _, err := Require(WithPrincipal(context.Background(), p)); !errors.Is(err, ErrUnauthenticated) {
			t.Fatal(err)
		}
	}
	for _, role := range []Role{Intern, Mentor} {
		p := Principal{UserID: "u", TeamID: "t", Role: role}
		got, err := Require(WithPrincipal(context.Background(), p))
		if err != nil || got != p {
			t.Fatalf("got %v, %v", got, err)
		}
	}
}
