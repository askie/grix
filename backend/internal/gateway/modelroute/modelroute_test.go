package modelroute

import "testing"

func TestCanonicalModel_DeepSeekAliases(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"deepseek-flash", "deepseek-flash"},
		{"deepseek-v4-flash", "deepseek-flash"},
		{"deepseek-v4-flash-vision-exp", "deepseek-flash"},
		{"deepseek-chat", "deepseek-flash"},
		{"deepseek-reasoner", "deepseek-v4-pro"},
		{"deepseek-v4-pro", "deepseek-v4-pro"},
	}
	for _, tc := range cases {
		if got := CanonicalModel("deepseek", tc.in); got != tc.want {
			t.Fatalf("CanonicalModel(%q)=%q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestRoutable_DeepSeekFlash(t *testing.T) {
	if !Routable("deepseek-flash") {
		t.Fatal("deepseek-flash should be routable")
	}
	if ResolveProvider("deepseek-flash") != "deepseek" {
		t.Fatal("deepseek-flash should route to deepseek")
	}
}
