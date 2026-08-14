package grixactions

import "testing"

func TestParseSessionControlCommand(t *testing.T) {
	tests := []struct {
		name      string
		raw       string
		wantVerb  string
		wantCwd   string
		wantMatch bool
		wantErr   bool
	}{
		{
			name:      "open session uri",
			raw:       BuildOpenSessionSubmitURI(OpenSessionSubmit{Cwd: "/workspace/project"}),
			wantVerb:  "open",
			wantCwd:   "/workspace/project",
			wantMatch: true,
		},
		{
			name:      "plain text /grix open not matched",
			raw:       "/grix open /workspace/demo",
			wantMatch: false,
		},
		{
			name:      "plain text /grix status not matched",
			raw:       "/grix status",
			wantMatch: false,
		},
		{
			name:      "plain text grix where not matched",
			raw:       "grix where",
			wantMatch: false,
		},
		{
			name:      "plain text /grix open no cwd not matched",
			raw:       "/grix open",
			wantMatch: false,
		},
		{
			name:      "unmatched command",
			raw:       "/grix hello",
			wantMatch: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, matched, err := ParseSessionControlCommand(tc.raw)
			if matched != tc.wantMatch {
				t.Fatalf("matched=%v want=%v", matched, tc.wantMatch)
			}
			if (err != nil) != tc.wantErr {
				t.Fatalf("err=%v wantErr=%v", err, tc.wantErr)
			}
			if got.Verb != tc.wantVerb {
				t.Fatalf("verb=%q want=%q", got.Verb, tc.wantVerb)
			}
			if got.Cwd != tc.wantCwd {
				t.Fatalf("cwd=%q want=%q", got.Cwd, tc.wantCwd)
			}
		})
	}
}
