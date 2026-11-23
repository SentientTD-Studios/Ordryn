package config

import (
	"os"
	"path/filepath"
	"testing"
)

func boolPtr(v bool) *bool { return &v }

func TestLoadDotEnvRequiresFile(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	SetRequireDotEnvForTest(boolPtr(true))
	t.Cleanup(func() { SetRequireDotEnvForTest(nil) })

	err := LoadDotEnv()
	if err == nil {
		t.Fatal("expected error when .env is missing")
	}
}

func TestLoadDotEnvSucceedsWithFile(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.WriteFile(".env", []byte("SESSION_KEY=test-session-key-for-unit-tests-32chars!!\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	SetRequireDotEnvForTest(boolPtr(true))
	t.Cleanup(func() { SetRequireDotEnvForTest(nil) })

	if err := LoadDotEnv(); err != nil {
		t.Fatalf("LoadDotEnv: %v", err)
	}
}

func TestValidateRequiredMissingVars(t *testing.T) {
	t.Setenv("DB_HOST", "")
	t.Setenv("DB_PORT", "")
	t.Setenv("DB_USER", "")
	t.Setenv("DB_PASSWORD", "")
	t.Setenv("DB_NAME", "")
	t.Setenv("SESSION_KEY", "")
	t.Setenv("REDIS_URL", "")
	t.Setenv("REDIS_ADDR", "")

	SetSkipRequiredForTest(boolPtr(false))
	t.Cleanup(func() { SetSkipRequiredForTest(nil) })

	err := ValidateRequired()
	if err == nil {
		t.Fatal("expected missing required vars error")
	}
}

func TestValidateRequiredAcceptsRedisAddr(t *testing.T) {
	t.Setenv("DB_HOST", "localhost")
	t.Setenv("DB_PORT", "5432")
	t.Setenv("DB_USER", "u")
	t.Setenv("DB_PASSWORD", "p")
	t.Setenv("DB_NAME", "db")
	t.Setenv("SESSION_KEY", "test-session-key-for-unit-tests-32chars!!")
	t.Setenv("REDIS_URL", "")
	t.Setenv("REDIS_ADDR", "localhost:6379")

	SetSkipRequiredForTest(boolPtr(false))
	t.Cleanup(func() { SetSkipRequiredForTest(nil) })

	if err := ValidateRequired(); err != nil {
		t.Fatalf("ValidateRequired: %v", err)
	}
}

func TestEnvWinsOverConfigJSONBasePath(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	if err := os.MkdirAll("config", 0o755); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join("config", "config.json")
	if err := os.WriteFile(cfgPath, []byte(`{"basePath":"/gotodo","useHttps":true,"siteName":"FromJSON"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(".env", []byte("SESSION_KEY=test-session-key-for-unit-tests-32chars!!\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("BASE_PATH", "/")
	t.Setenv("USE_HTTPS", "false")
	t.Setenv("SITE_NAME", "FromEnv")
	t.Setenv("DB_HOST", "localhost")
	t.Setenv("DB_PORT", "5432")
	t.Setenv("DB_USER", "u")
	t.Setenv("DB_PASSWORD", "p")
	t.Setenv("DB_NAME", "db")
	t.Setenv("SESSION_KEY", "test-session-key-for-unit-tests-32chars!!")
	t.Setenv("REDIS_URL", "redis://localhost:6379/0")

	SetRequireDotEnvForTest(boolPtr(true))
	SetSkipRequiredForTest(boolPtr(false))
	t.Cleanup(func() {
		SetRequireDotEnvForTest(nil)
		SetSkipRequiredForTest(nil)
	})

	if err := Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if Cfg.BasePath != "/" {
		t.Fatalf("BasePath = %q, want / (env must win over JSON /gotodo)", Cfg.BasePath)
	}
	if Cfg.UseHTTPS {
		t.Fatal("UseHTTPS should be false from env")
	}
	if Cfg.SiteName != "FromEnv" {
		t.Fatalf("SiteName = %q, want FromEnv", Cfg.SiteName)
	}
}

func TestJSONFillsWhenEnvUnset(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	if err := os.MkdirAll("config", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join("config", "config.json"), []byte(`{"siteName":"FromJSON","basePath":"/gotodo"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(".env", []byte("SESSION_KEY=test-session-key-for-unit-tests-32chars!!\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("BASE_PATH", "")
	t.Setenv("SITE_NAME", "")
	t.Setenv("DB_HOST", "localhost")
	t.Setenv("DB_PORT", "5432")
	t.Setenv("DB_USER", "u")
	t.Setenv("DB_PASSWORD", "p")
	t.Setenv("DB_NAME", "db")
	t.Setenv("SESSION_KEY", "test-session-key-for-unit-tests-32chars!!")
	t.Setenv("REDIS_URL", "redis://localhost:6379/0")

	SetRequireDotEnvForTest(boolPtr(true))
	SetSkipRequiredForTest(boolPtr(false))
	t.Cleanup(func() {
		SetRequireDotEnvForTest(nil)
		SetSkipRequiredForTest(nil)
	})

	if err := Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if Cfg.BasePath != "/gotodo" {
		t.Fatalf("BasePath = %q, want /gotodo from JSON", Cfg.BasePath)
	}
	if Cfg.SiteName != "FromJSON" {
		t.Fatalf("SiteName = %q, want FromJSON", Cfg.SiteName)
	}
}

func TestStateStoreBackend(t *testing.T) {
	tests := []struct {
		store, url, addr string
		want             string
		wantErr          bool
	}{
		{"", "", "", StateStorePostgres, false},
		{"", "redis://localhost:6379/0", "", StateStoreRedis, false},
		{"", "", "localhost:6379", StateStoreRedis, false},
		{"postgres", "redis://localhost:6379/0", "", StateStorePostgres, false},
		{"PG", "", "", StateStorePostgres, false},
		{"redis", "", "", "", true},
		{"redis", "redis://localhost:6379/0", "", StateStoreRedis, false},
		{"memcached", "", "", "", true},
	}
	for _, tt := range tests {
		t.Setenv("STATE_STORE", tt.store)
		t.Setenv("REDIS_URL", tt.url)
		t.Setenv("REDIS_ADDR", tt.addr)
		got, err := StateStoreBackend()
		if (err != nil) != tt.wantErr || got != tt.want {
			t.Errorf("STATE_STORE=%q url=%q addr=%q: got %q, %v; want %q (err=%v)",
				tt.store, tt.url, tt.addr, got, err, tt.want, tt.wantErr)
		}
	}
}

func TestValidateRequiredWithoutRedis(t *testing.T) {
	t.Setenv("DB_HOST", "localhost")
	t.Setenv("DB_PORT", "5432")
	t.Setenv("DB_USER", "u")
	t.Setenv("DB_PASSWORD", "p")
	t.Setenv("DB_NAME", "db")
	t.Setenv("SESSION_KEY", "test-session-key-for-unit-tests-32chars!!")
	t.Setenv("REDIS_URL", "")
	t.Setenv("REDIS_ADDR", "")
	t.Setenv("STATE_STORE", "")

	SetSkipRequiredForTest(boolPtr(false))
	t.Cleanup(func() { SetSkipRequiredForTest(nil) })

	if err := ValidateRequired(); err != nil {
		t.Fatalf("ValidateRequired: %v", err)
	}
	t.Setenv("STATE_STORE", "redis")
	if err := ValidateRequired(); err == nil {
		t.Fatal("STATE_STORE=redis without REDIS_URL should fail")
	}
}
