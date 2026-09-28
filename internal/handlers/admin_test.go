package handlers

import (
	"testing"

	"commonplace/internal/auth"
)

func TestIsAdmin(t *testing.T) {
	const (
		handle = "shawn"
		email  = "shawn@example.com"
	)
	cases := []struct {
		name      string
		cfgHandle string
		cfgEmail  string
		user      *auth.User
		want      bool
	}{
		{"nil user", handle, email, nil, false},
		{"unconfigured", "", "", &auth.User{Handle: handle, Email: email}, false},
		{"handle only configured", handle, "", &auth.User{Handle: handle, Email: email}, false},
		{"email only configured", "", email, &auth.User{Handle: handle, Email: email}, false},
		{"both match", handle, email, &auth.User{Handle: handle, Email: email}, true},
		// A squatter who renamed into the admin handle still fails the email check.
		{"handle squatter", handle, email, &auth.User{Handle: handle, Email: "eve@example.com"}, false},
		// The real admin after renaming away no longer matches either.
		{"admin renamed away", handle, email, &auth.User{Handle: "shawn2", Email: email}, false},
		{"email case folds", handle, email, &auth.User{Handle: handle, Email: "Shawn@Example.com"}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := &Server{AdminHandle: c.cfgHandle, AdminEmail: c.cfgEmail}
			if got := s.IsAdmin(c.user); got != c.want {
				t.Fatalf("IsAdmin = %v, want %v", got, c.want)
			}
		})
	}
}
