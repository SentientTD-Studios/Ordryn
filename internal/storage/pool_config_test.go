package storage

import "testing"

const testDSN = "postgres://u:p@localhost:5432/db?sslmode=disable"

func TestPoolConfigDefaults(t *testing.T) {
	t.Setenv("DB_MAX_CONNS", "")
	t.Setenv("DB_STATEMENT_TIMEOUT", "")
	t.Setenv("DB_IDLE_IN_TX_TIMEOUT", "")
	cfg, err := poolConfig(testDSN)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MaxConns != defaultDBMaxConns {
		t.Errorf("MaxConns = %d, want %d", cfg.MaxConns, defaultDBMaxConns)
	}
	p := cfg.ConnConfig.RuntimeParams
	if p["statement_timeout"] != defaultStatementTimeout {
		t.Errorf("statement_timeout = %q", p["statement_timeout"])
	}
	if p["idle_in_transaction_session_timeout"] != defaultIdleInTxTimeout {
		t.Errorf("idle_in_transaction_session_timeout = %q", p["idle_in_transaction_session_timeout"])
	}
}

func TestPoolConfigOverrides(t *testing.T) {
	t.Setenv("DB_MAX_CONNS", "7")
	t.Setenv("DB_STATEMENT_TIMEOUT", "0")
	t.Setenv("DB_IDLE_IN_TX_TIMEOUT", "30s")
	cfg, err := poolConfig(testDSN)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MaxConns != 7 {
		t.Errorf("MaxConns = %d, want 7", cfg.MaxConns)
	}
	if got := cfg.ConnConfig.RuntimeParams["statement_timeout"]; got != "0" {
		t.Errorf("statement_timeout = %q, want 0", got)
	}
	if got := cfg.ConnConfig.RuntimeParams["idle_in_transaction_session_timeout"]; got != "30s" {
		t.Errorf("idle_in_transaction_session_timeout = %q, want 30s", got)
	}
}

func TestPoolConfigRejectsBadMaxConns(t *testing.T) {
	for _, v := range []string{"0", "-1", "lots"} {
		t.Setenv("DB_MAX_CONNS", v)
		if _, err := poolConfig(testDSN); err == nil {
			t.Errorf("DB_MAX_CONNS=%q: expected error", v)
		}
	}
}
