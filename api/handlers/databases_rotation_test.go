package handlers

import "testing"

func TestBuildAlterRolePasswordSQL(t *testing.T) {
	sql, err := buildAlterRolePasswordSQL(`app"user`, "pa'ss")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := `ALTER ROLE "app""user" WITH PASSWORD 'pa''ss'`
	if sql != want {
		t.Fatalf("sql=%q want %q", sql, want)
	}
}

func TestBuildAlterRolePasswordSQLErrors(t *testing.T) {
	if _, err := buildAlterRolePasswordSQL("", "abcdef0123456789"); err == nil {
		t.Fatalf("expected error for missing username")
	}
	if _, err := buildAlterRolePasswordSQL("app", ""); err == nil {
		t.Fatalf("expected error for missing password")
	}
}
