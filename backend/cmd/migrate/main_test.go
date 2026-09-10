package main

import "testing"

// TestParseArgs covers the --backfill-provider-keys opt-in flag: off by
// default so a routine deploy's plain `migrate config.yaml` invocation never
// runs the opencode/deepseek/deveco provider_key backfill; on only when
// explicitly passed, with the config path still resolved correctly either
// way.
func TestParseArgs(t *testing.T) {
	cases := []struct {
		name           string
		args           []string
		wantConfigPath string
		wantBackfill   bool
	}{
		{
			name:           "no args uses the default config path and stays off",
			args:           nil,
			wantConfigPath: "config.yaml",
			wantBackfill:   false,
		},
		{
			name:           "positional config path only, unaffected by the flag default",
			args:           []string{"custom.yaml"},
			wantConfigPath: "custom.yaml",
			wantBackfill:   false,
		},
		{
			name:           "flag before the positional config path",
			args:           []string{"--backfill-provider-keys", "custom.yaml"},
			wantConfigPath: "custom.yaml",
			wantBackfill:   true,
		},
		{
			name:           "flag alone keeps the default config path",
			args:           []string{"--backfill-provider-keys"},
			wantConfigPath: "config.yaml",
			wantBackfill:   true,
		},
		{
			name:           "explicit =false stays off",
			args:           []string{"--backfill-provider-keys=false", "custom.yaml"},
			wantConfigPath: "custom.yaml",
			wantBackfill:   false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			gotConfigPath, gotBackfill := parseArgs(c.args)
			if gotConfigPath != c.wantConfigPath {
				t.Errorf("configPath = %q, want %q", gotConfigPath, c.wantConfigPath)
			}
			if gotBackfill != c.wantBackfill {
				t.Errorf("backfillProviderKeys = %v, want %v", gotBackfill, c.wantBackfill)
			}
		})
	}
}
