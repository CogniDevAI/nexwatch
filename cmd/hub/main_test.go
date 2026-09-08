package main

import "testing"

func TestShouldForceServe(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want bool
	}{
		{"bare invocation", []string{"nexwatch-hub"}, true},
		{"only --http flag", []string{"nexwatch-hub", "--http=0.0.0.0:9090"}, true},
		{"short flag form", []string{"nexwatch-hub", "-http=0.0.0.0:9090"}, true},
		{"explicit serve subcommand", []string{"nexwatch-hub", "serve"}, false},
		{"superuser upsert subcommand", []string{"nexwatch-hub", "superuser", "upsert", "a@b.com", "pw"}, false},
		{"migrate subcommand", []string{"nexwatch-hub", "migrate"}, false},
		{"empty args slice", []string{}, true},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldForceServe(tt.args); got != tt.want {
				t.Fatalf("shouldForceServe(%v) = %v, want %v", tt.args, got, tt.want)
			}
		})
	}
}
