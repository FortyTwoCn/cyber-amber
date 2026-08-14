package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDefaultsAndEnvironment(t *testing.T) {
	t.Setenv("APP_ADDR", "127.0.0.1:9999")
	t.Setenv("DEFAULT_FPS", "12")
	t.Setenv("BILI_PUBLISH_CONCURRENCY", "3")
	t.Setenv("DEFAULT_DANMAKU_OPACITY", "0.65")
	t.Setenv("MIN_FREE_DISK_BYTES", "4096")
	t.Setenv("TRUSTED_PROXY_CIDRS", "10.0.0.0/8, 192.0.2.0/24")
	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.App.Addr != "127.0.0.1:9999" || cfg.Defaults.FPS != 12 || cfg.Jobs.PublishConcurrency != 3 || cfg.Defaults.DanmakuOpacity != .65 || cfg.Limits.MinFreeDiskBytes != 4096 || len(cfg.Web.TrustedProxyCIDRs) != 2 {
		t.Fatalf("environment not applied: %#v", cfg)
	}
}

func TestRejectsUnsafeCrossFieldConfiguration(t *testing.T) {
	t.Setenv("JOB_TIMEOUT", "10s")
	t.Setenv("JOB_LEASE_DURATION", "11s")
	t.Setenv("MIN_WIDTH", "321")
	if _, err := Load(""); err == nil {
		t.Fatal("expected invalid lease and odd minimum width to fail")
	}
}

func TestCookieRequiresEncryptionKey(t *testing.T) {
	t.Setenv("BILI_COOKIE", "SESSDATA=secret")
	_, err := Load("")
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestSecretFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(path, []byte("abcdefghijklmnopqrstuvwxyz123456"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SESSION_SECRET_FILE", path)
	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Security.SessionSecret) != 32 {
		t.Fatalf("got %d secret bytes", len(cfg.Security.SessionSecret))
	}
}

func TestOperationalSettingsApplyAtomically(t *testing.T) {
	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	settings := cfg.OperationalSettings()
	settings.DefaultFPS = 12
	settings.MaxClipDuration = "12s"
	if err := cfg.ApplyOperationalSettings(settings); err != nil {
		t.Fatal(err)
	}
	if cfg.Defaults.FPS != 12 || cfg.Limits.MaxClipDurationText != "12s" || cfg.Limits.MaxClipDuration.String() != "12s" {
		t.Fatalf("settings were not normalized: %#v", cfg)
	}
	unchanged := cfg
	invalid := cfg.OperationalSettings()
	invalid.DefaultFPS = invalid.MaxOutputFPS + 1
	if err := cfg.ApplyOperationalSettings(invalid); err == nil {
		t.Fatal("expected invalid settings to be rejected")
	}
	if cfg.Defaults.FPS != unchanged.Defaults.FPS || cfg.Limits.MaxOutputFPS != unchanged.Limits.MaxOutputFPS {
		t.Fatal("invalid settings partially mutated the configuration")
	}
}
