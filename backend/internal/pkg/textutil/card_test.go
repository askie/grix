package textutil

import "testing"

func TestIsStandaloneCardMessage(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   string
		want bool
	}{
		{
			name: "standalone tool card",
			in:   "[工具执行](grix://card/tool_execution?id=1)",
			want: true,
		},
		{
			name: "standalone with surrounding whitespace",
			in:   "  [Thinking](grix://card/thinking?content=x) \n",
			want: true,
		},
		{
			name: "plain text",
			in:   "fixed the login bug",
			want: false,
		},
		{
			name: "text then card stays previewable",
			in:   "已修好登录\n[文件](grix://card/file?path=app.go)",
			want: false,
		},
		{
			name: "contains card substring in prose",
			in:   "see ](grix://card/tool_execution?id=1) in docs",
			want: false,
		},
		{
			name: "empty",
			in:   "  ",
			want: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := IsStandaloneCardMessage(tc.in); got != tc.want {
				t.Fatalf("IsStandaloneCardMessage(%q)=%v want %v", tc.in, got, tc.want)
			}
		})
	}
}
