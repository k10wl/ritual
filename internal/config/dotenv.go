package config

import (
	"log"
	"os"
	"path/filepath"

	"github.com/joho/godotenv"
)

// EnvProfile selects which .env.{profile}.local file LoadEnvFiles reads.
// Mirrors the Taskfile's RITUAL_ENV variable (default "dev"), so a single
// .env.dev.local works the same way whether the GUI is launched via
// `task gui:dev` or by double-clicking the built binary.
const EnvProfile = "RITUAL_ENV"

// DefaultEnvProfile matches Taskfile.yml's `RITUAL_ENV: {{.RITUAL_ENV | default "dev"}}`.
const DefaultEnvProfile = "dev"

// taskfileToRuntime maps the Cloudflare-dashboard variable names that
// `.env.{dev,prod}.local` files hold (R2_BUCKET_NAME, ...) onto the
// RITUAL_R2_* names the remote subsystem reads. Mirrors the Taskfile's
// top-level `env:` block. Process-env values already set under the
// RITUAL_R2_* names are never overwritten — operator shell wins.
var taskfileToRuntime = map[string]string{
	"R2_BUCKET_NAME":       "RITUAL_R2_BUCKET",
	"R2_ACCOUNT_ID":        "RITUAL_R2_ACCOUNT_ID",
	"R2_ACCESS_KEY_ID":     "RITUAL_R2_ACCESS_KEY_ID",
	"R2_SECRET_ACCESS_KEY": "RITUAL_R2_SECRET_ACCESS_KEY",
}

// LoadEnvFiles reads .env.{profile}.local (profile = $RITUAL_ENV, default
// "dev") from the executable's directory and the current working
// directory. First file found wins; remaining candidates are ignored.
//
// Already-set process env vars are never overwritten (godotenv.Load
// semantics), so an operator's shell — or CI's secret store — beats any
// file. After loading, R2_* dashboard names are mirrored onto RITUAL_R2_*
// runtime names if the latter aren't already set, matching the Taskfile
// env-remap block. Failures are logged and non-fatal; missing creds
// surface later as remote.ErrR2EnvIncomplete at boot.
//
// Call once, very early in main(), before any subsystem reads env vars.
func LoadEnvFiles() {
	for _, p := range envCandidatePaths() {
		if _, err := os.Stat(p); err != nil {
			continue
		}
		if err := godotenv.Load(p); err != nil {
			log.Printf("dotenv: load %s: %v (continuing)", p, err)
			continue
		}
		log.Printf("dotenv: loaded %s", p)
		break
	}
	mirrorTaskfileNames()
}

func envCandidatePaths() []string {
	profile := os.Getenv(EnvProfile)
	if profile == "" {
		profile = DefaultEnvProfile
	}
	name := ".env." + profile + ".local"

	var out []string
	if exe, err := os.Executable(); err == nil {
		out = append(out, filepath.Join(filepath.Dir(exe), name))
	}
	if cwd, err := os.Getwd(); err == nil {
		out = append(out, filepath.Join(cwd, name))
	}
	return out
}

func mirrorTaskfileNames() {
	for src, dst := range taskfileToRuntime {
		if os.Getenv(dst) != "" {
			continue
		}
		if v := os.Getenv(src); v != "" {
			_ = os.Setenv(dst, v)
		}
	}
}
