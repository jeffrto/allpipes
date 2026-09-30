package config

import "testing"

func TestNormalizeGroup(t *testing.T) {
	cases := []struct {
		in, base, want string
	}{
		{"acme", "https://gitlab.com", "acme"},
		{" acme/platform/ ", "https://gitlab.com", "acme/platform"},
		{"42", "https://gitlab.com", "42"},
		{"https://gitlab.com/acme/platform", "https://gitlab.com", "acme/platform"},
		{"https://gitlab.com/groups/acme/-/epics", "https://gitlab.com", "acme"},
		{"https://git.example.com/gitlab/acme", "https://git.example.com/gitlab", "acme"},
	}
	for _, c := range cases {
		if got := normalizeGroup(c.in, c.base); got != c.want {
			t.Errorf("normalizeGroup(%q, %q) = %q, want %q", c.in, c.base, got, c.want)
		}
	}
}
