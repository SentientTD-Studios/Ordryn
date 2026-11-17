package domain

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// usePGDump points PG_DUMP_PATH at a local pg_dump, or skips the test.
func usePGDump(t *testing.T) {
	t.Helper()
	if _, err := pgDumpPath(); err == nil {
		return
	}
	for _, p := range []string{`C:\Program Files\PostgreSQL\18\bin\pg_dump.exe`, `C:\Program Files\PostgreSQL\17\bin\pg_dump.exe`, `C:\Program Files\PostgreSQL\16\bin\pg_dump.exe`} {
		if _, err := os.Stat(p); err == nil {
			t.Setenv("PG_DUMP_PATH", p)
			return
		}
	}
	if _, err := exec.LookPath("pg_dump"); err != nil {
		t.Skip("pg_dump not installed")
	}
}

func TestWriteBackupArchive(t *testing.T) {
	setupCompanySite(t, "gotodo_backup_test")
	usePGDump(t)

	var buf bytes.Buffer
	if err := WriteBackup(context.Background(), &buf); err != nil {
		t.Fatal(err)
	}
	gz, err := gzip.NewReader(&buf)
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(gz)
	found := map[string]int64{}
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		n, _ := io.Copy(io.Discard, tr)
		found[h.Name] = n
	}
	if found["database.dump"] < 1000 || found["manifest.json"] == 0 {
		t.Fatalf("archive contents: %v", found)
	}
}

func TestScheduledBackupsRotateAndValidateNames(t *testing.T) {
	setupCompanySite(t, "gotodo_backup_rotate_test")
	usePGDump(t)
	dir := t.TempDir()
	// Older backups already in the folder.
	for _, name := range []string{"ordryn-backup-20200101-000000.tar.gz", "ordryn-backup-20200102-000000.tar.gz"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("keep me"), 0o600); err != nil {
		t.Fatal(err)
	}
	name, err := RunScheduledBackup(context.Background(), BackupSchedule{Dir: dir, IntervalHours: 24, Keep: 2})
	if err != nil {
		t.Fatal(err)
	}
	files, err := ListBackupFiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 || files[0].Name != name || files[1].Name != "ordryn-backup-20200102-000000.tar.gz" {
		t.Fatalf("after rotation: %+v", files)
	}
	if _, err := os.Stat(filepath.Join(dir, "notes.txt")); err != nil {
		t.Fatal("unrelated files must be left alone")
	}
	if _, err := OpenBackupFile(dir, "../notes.txt"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("path escape: %v", err)
	}
	f, err := OpenBackupFile(dir, name)
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
}
