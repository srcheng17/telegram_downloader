package config

import "testing"

func TestKomgaMappingsRejectUnsafePaths(t *testing.T) {
	cases := []string{
		`{"library_id":"a","komga_root":"/books","local_root":"/mnt/books"}`,
		`[{"library_id":"a","komga_root":"/books/../other","local_root":"/mnt/books"}]`,
		`[{"library_id":"a","komga_root":"/books","local_root":"relative"}]`,
		`[{"library_id":"a","komga_root":"/books","local_root":"/mnt/books"},{"library_id":"a","komga_root":"/other","local_root":"/mnt/other"}]`,
	}
	for _, raw := range cases {
		if _, err := parseKomgaLibraryMappings(raw); err == nil {
			t.Fatalf("accepted unsafe mapping: %s", raw)
		}
	}
	good := `[{"library_id":"lib-1","komga_root":"/books","local_root":"/mnt/books"}]`
	items, err := parseKomgaLibraryMappings(good)
	if err != nil || len(items) != 1 {
		t.Fatalf("valid mapping rejected: %v", err)
	}
	if err := validateKomgaBackupRoot("/mnt/books/private", items); err == nil {
		t.Fatal("backup inside Komga mount accepted")
	}
	if err := validateKomgaBackupRoot("/private/komga-backups", items); err != nil {
		t.Fatalf("private backup rejected: %v", err)
	}
	if _, err := parseKomgaReadOnlyLibraries(`["lib-1"]`, items); err == nil {
		t.Fatal("read-only allowlist duplicated a writable library")
	}
	if ids, err := parseKomgaReadOnlyLibraries(`["lib-2"]`, items); err != nil || len(ids) != 1 {
		t.Fatalf("read-only library rejected: %v", err)
	}
}
