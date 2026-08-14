package grixactions

import "testing"

func TestParseQuestionReply(t *testing.T) {
	raw := BuildQuestionReplyURI(QuestionReply{
		RequestID: "req-1",
		Response: map[string]any{
			"type": "map",
			"entries": []map[string]any{
				{"key": "1", "value": "prod"},
				{"key": "2", "value": "cn-hz"},
			},
		},
	})

	reply, matched, err := ParseQuestionReply(raw)
	if err != nil || !matched {
		t.Fatalf("matched=%v err=%v", matched, err)
	}
	if reply.RequestID != "req-1" {
		t.Fatalf("request_id=%q want=req-1", reply.RequestID)
	}
	if reply.Response["type"] != "map" {
		t.Fatalf("response=%#v", reply.Response)
	}
}

func TestParseOpenSessionSubmit(t *testing.T) {
	tests := []struct {
		name               string
		raw                string
		wantCwd            string
		wantCardInstanceID string
		wantMatch          bool
		wantErr            bool
	}{
		{
			name: "new open session uri",
			raw: BuildOpenSessionSubmitURI(OpenSessionSubmit{
				Cwd:            "/workspace/demo",
				CardInstanceID: "card-open-1",
			}),
			wantCwd:            "/workspace/demo",
			wantCardInstanceID: "card-open-1",
			wantMatch:          true,
		},
		{
			name:      "open session missing cwd",
			raw:       "grix://open/session",
			wantMatch: true,
			wantErr:   true,
		},
		{
			name:      "legacy card action uri no longer matches",
			raw:       "grix://card/agent_open_session_submit?cwd=%2Fworkspace%2Flegacy",
			wantMatch: false,
		},
		{
			name:      "unmatched uri",
			raw:       "grix://card/agent_question_reply?d=%7B%7D",
			wantMatch: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, matched, err := ParseOpenSessionSubmit(tc.raw)
			if matched != tc.wantMatch {
				t.Fatalf("matched=%v want=%v", matched, tc.wantMatch)
			}
			if (err != nil) != tc.wantErr {
				t.Fatalf("err=%v wantErr=%v", err, tc.wantErr)
			}
			if got.Cwd != tc.wantCwd {
				t.Fatalf("cwd=%q want=%q", got.Cwd, tc.wantCwd)
			}
			if got.CardInstanceID != tc.wantCardInstanceID {
				t.Fatalf("card_instance_id=%q want=%q", got.CardInstanceID, tc.wantCardInstanceID)
			}
		})
	}
}

func TestRewriteToLegacyCommand(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "question single",
			raw: BuildQuestionReplyURI(QuestionReply{
				RequestID: "req-1",
				Response: map[string]any{
					"type":  "single",
					"value": "production",
				},
			}),
			want: "/grix question req-1 production",
		},
		{
			name: "question accept",
			raw: BuildQuestionReplyURI(QuestionReply{
				RequestID: "req-2",
				Action:    "accept",
			}),
			want: "/grix question req-2 __grix_accept__",
		},
		{
			name: "open session",
			raw:  BuildOpenSessionSubmitURI(OpenSessionSubmit{Cwd: "/workspace/demo"}),
			want: "/grix open /workspace/demo",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := RewriteToLegacyCommand(tc.raw); got != tc.want {
				t.Fatalf("got=%q want=%q", got, tc.want)
			}
		})
	}
}
