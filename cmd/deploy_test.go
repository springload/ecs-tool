package cmd

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// newDeployTimeoutCmd gives a fresh command bound like deployCmd, with clean viper and env state.
func newDeployTimeoutCmd(t *testing.T) *cobra.Command {
	t.Helper()
	viper.Reset()
	t.Setenv("ECS_DEPLOY_TIMEOUT", "")
	if err := os.Unsetenv("ECS_DEPLOY_TIMEOUT"); err != nil {
		t.Fatal(err)
	}
	cmd := &cobra.Command{Use: "deploy"}
	if err := bindDeployTimeout(cmd); err != nil {
		t.Fatal(err)
	}
	return cmd
}

func useConfig(t *testing.T, contents string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ecs.toml")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	viper.SetConfigFile(path)
	if err := viper.ReadInConfig(); err != nil {
		t.Fatal(err)
	}
}

func TestDeployTimeoutDefaultsTo10m(t *testing.T) {
	newDeployTimeoutCmd(t)

	got, err := deployTimeout()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != 10*time.Minute {
		t.Fatalf("expected default 10m, got %v", got)
	}
}

func TestDeployTimeoutFromFlag(t *testing.T) {
	cmd := newDeployTimeoutCmd(t)
	if err := cmd.ParseFlags([]string{"--timeout=20m"}); err != nil {
		t.Fatal(err)
	}

	got, err := deployTimeout()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != 20*time.Minute {
		t.Fatalf("expected flag value 20m, got %v", got)
	}
}

func TestDeployTimeoutFromTOML(t *testing.T) {
	newDeployTimeoutCmd(t)
	useConfig(t, "[deploy]\ntimeout = \"25m\"\n")

	got, err := deployTimeout()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != 25*time.Minute {
		t.Fatalf("expected config value 25m, got %v", got)
	}
}

func TestDeployTimeoutFromEnvVar(t *testing.T) {
	newDeployTimeoutCmd(t)
	t.Setenv("ECS_DEPLOY_TIMEOUT", "15m")

	got, err := deployTimeout()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != 15*time.Minute {
		t.Fatalf("expected env value 15m, got %v", got)
	}
}

func TestDeployTimeoutPrecedence(t *testing.T) {
	cases := []struct {
		name   string
		flag   []string
		env    string
		config string
		want   time.Duration
	}{
		{name: "flag beats env, config and default", flag: []string{"--timeout=1m"}, env: "2m", config: "3m", want: time.Minute},
		{name: "env beats config and default", env: "2m", config: "3m", want: 2 * time.Minute},
		{name: "config beats default", config: "3m", want: 3 * time.Minute},
		{name: "default when nothing is set", want: 10 * time.Minute},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd := newDeployTimeoutCmd(t)
			if tc.env != "" {
				t.Setenv("ECS_DEPLOY_TIMEOUT", tc.env)
			}
			if tc.config != "" {
				useConfig(t, "[deploy]\ntimeout = \""+tc.config+"\"\n")
			}
			if len(tc.flag) > 0 {
				if err := cmd.ParseFlags(tc.flag); err != nil {
					t.Fatal(err)
				}
			}

			got, err := deployTimeout()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("expected %v, got %v", tc.want, got)
			}
		})
	}
}

func TestDeployTimeoutRejectsInvalidValues(t *testing.T) {
	cases := map[string][]string{
		"unparseable": {"--timeout=soon"},
		"empty":       {"--timeout="},
		"zero":        {"--timeout=0s"},
		"negative":    {"--timeout=-5m"},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			cmd := newDeployTimeoutCmd(t)
			if err := cmd.ParseFlags(args); err != nil {
				t.Fatal(err)
			}
			if got, err := deployTimeout(); err == nil {
				t.Fatalf("expected an error, got timeout %v", got)
			}
		})
	}

	t.Run("invalid env var", func(t *testing.T) {
		newDeployTimeoutCmd(t)
		t.Setenv("ECS_DEPLOY_TIMEOUT", "forever")
		if _, err := deployTimeout(); err == nil {
			t.Fatal("expected an error for an invalid ECS_DEPLOY_TIMEOUT")
		}
	})
}
