package store

import (
	"path/filepath"
	"testing"
)

// TestOpenMigrateSeed 验证：建库 → 基线表 → 费率/科目种子幂等。
func TestOpenMigrateSeed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "finlite_test.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	var n int
	if err := db.Pool().QueryRow(`SELECT COUNT(*) FROM fee_rates`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 4 {
		t.Fatalf("fee_rates = %d, want 4", n)
	}
	if err := db.Pool().QueryRow(`SELECT COUNT(*) FROM accounts`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 6 {
		t.Fatalf("accounts = %d, want 6", n)
	}
	// 二次打开幂等
	db2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()
}
