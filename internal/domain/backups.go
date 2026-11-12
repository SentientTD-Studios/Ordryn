package domain

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"GoTodo/internal/imagehost"
	"GoTodo/internal/storage"
	"GoTodo/internal/version"
)

// ErrNoPGDump means pg_dump isn't installed where the server can run it.
var ErrNoPGDump = errors.New("pg_dump not found")

// pgDumpPath finds pg_dump: PG_DUMP_PATH, else PATH.
func pgDumpPath() (string, error) {
	if p := strings.TrimSpace(os.Getenv("PG_DUMP_PATH")); p != "" {
		if _, err := os.Stat(p); err != nil {
			return "", ErrNoPGDump
		}
		return p, nil
	}
	p, err := exec.LookPath("pg_dump")
	if err != nil {
		return "", ErrNoPGDump
	}
	return p, nil
}

// PGDumpVersion reports pg_dump's version, or "" when it isn't available.
func PGDumpVersion(ctx context.Context) string {
	p, err := pgDumpPath()
	if err != nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, p, "--version").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// backupManifest describes what's in an archive.
type backupManifest struct {
	Format      int       `json:"format"`
	CreatedAt   time.Time `json:"created_at"`
	SiteVersion string    `json:"site_version"`
	SiteName    string    `json:"site_name"`
	Database    string    `json:"database"`
	Uploads     int       `json:"uploads"`
	Restore     string    `json:"restore"`
}

const restoreHint = "Turn on maintenance mode, stop the server, then: " +
	"pg_restore --clean --if-exists --no-owner -d <database> database.dump; copy uploads/ back to the image folder; start the server."

// WriteBackup streams a .tar.gz with manifest.json, database.dump (pg_dump
// custom format), and uploads/ (local image hosting only).
func WriteBackup(ctx context.Context, w io.Writer) error {
	dumpPath, err := pgDumpPath()
	if err != nil {
		return err
	}
	env, err := storage.PGEnv()
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp("", "ordryn-db-*.dump")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()

	cmd := exec.CommandContext(ctx, dumpPath, "--format=custom", "--no-owner", "--no-privileges")
	cmd.Env = append(os.Environ(), env...)
	cmd.Stdout = tmp
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("pg_dump failed: %v: %s", err, strings.TrimSpace(stderr.String()))
	}

	gz := gzip.NewWriter(w)
	tw := tar.NewWriter(gz)
	uploadsDir := localUploadsDir()
	uploads := 0

	addFile := func(name string, f *os.File) error {
		st, err := f.Stat()
		if err != nil {
			return err
		}
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: st.Size(), ModTime: st.ModTime()}); err != nil {
			return err
		}
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			return err
		}
		_, err = io.Copy(tw, f)
		return err
	}
	if err := addFile("database.dump", tmp); err != nil {
		return err
	}
	if uploadsDir != "" {
		err := filepath.WalkDir(uploadsDir, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || d.Type()&fs.ModeSymlink != 0 {
				return nil
			}
			rel, err := filepath.Rel(uploadsDir, path)
			if err != nil {
				return nil
			}
			f, err := os.Open(path)
			if err != nil {
				return nil
			}
			defer f.Close()
			if err := addFile("uploads/"+filepath.ToSlash(rel), f); err != nil {
				return err
			}
			uploads++
			return nil
		})
		if err != nil {
			return err
		}
	}
	m := backupManifest{Format: 1, CreatedAt: time.Now().UTC(), SiteVersion: version.Version,
		Database: "database.dump (pg_dump custom format)", Uploads: uploads, Restore: restoreHint}
	if s, err := storage.GetSiteSettings(); err == nil {
		m.SiteName = s.SiteName
	}
	mb, _ := json.MarshalIndent(m, "", "  ")
	if err := tw.WriteHeader(&tar.Header{Name: "manifest.json", Mode: 0o600, Size: int64(len(mb)), ModTime: m.CreatedAt}); err != nil {
		return err
	}
	if _, err := tw.Write(mb); err != nil {
		return err
	}
	if err := tw.Close(); err != nil {
		return err
	}
	return gz.Close()
}

// localUploadsDir is the image folder when images are stored on this server.
func localUploadsDir() string {
	s, err := storage.GetSiteSettings()
	if err != nil {
		return ""
	}
	cfg, err := s.ImageHostingConfig()
	if err != nil || cfg.Provider != imagehost.ProviderLocal {
		return ""
	}
	dir := s.Image.LocalPath
	if dir == "" {
		dir = imagehost.DefaultLocalPath
	}
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		return ""
	}
	return dir
}

// BackupSchedule is the automatic backup configuration from the environment:
// BACKUP_DIR, BACKUP_INTERVAL_HOURS (0 = off), BACKUP_KEEP (default 7).
type BackupSchedule struct {
	Dir           string `json:"dir"`
	IntervalHours int    `json:"interval_hours"`
	Keep          int    `json:"keep"`
}

// Enabled reports whether scheduled backups run.
func (b BackupSchedule) Enabled() bool { return b.Dir != "" && b.IntervalHours > 0 }

// LoadBackupSchedule reads the schedule from the environment.
func LoadBackupSchedule() BackupSchedule {
	b := BackupSchedule{Dir: strings.TrimSpace(os.Getenv("BACKUP_DIR")), Keep: 7}
	b.IntervalHours, _ = strconv.Atoi(strings.TrimSpace(os.Getenv("BACKUP_INTERVAL_HOURS")))
	if n, err := strconv.Atoi(strings.TrimSpace(os.Getenv("BACKUP_KEEP"))); err == nil && n > 0 {
		b.Keep = n
	}
	return b
}

// BackupFile is a stored backup.
type BackupFile struct {
	Name      string    `json:"name"`
	Size      int64     `json:"size"`
	CreatedAt time.Time `json:"created_at"`
}

var backupNameRe = regexp.MustCompile(`^ordryn-backup-\d{8}-\d{6}\.tar\.gz$`)

// ListBackupFiles lists stored backups, newest first.
func ListBackupFiles(dir string) ([]BackupFile, error) {
	out := []BackupFile{}
	if dir == "" {
		return out, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return out, nil
		}
		return nil, err
	}
	for _, e := range entries {
		if e.IsDir() || !backupNameRe.MatchString(e.Name()) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, BackupFile{Name: e.Name(), Size: info.Size(), CreatedAt: info.ModTime().UTC()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name > out[j].Name })
	return out, nil
}

// OpenBackupFile opens a stored backup by name; names are checked, so paths
// can't escape the folder.
func OpenBackupFile(dir, name string) (*os.File, error) {
	if dir == "" || !backupNameRe.MatchString(name) {
		return nil, ErrNotFound
	}
	f, err := os.Open(filepath.Join(dir, name))
	if err != nil {
		return nil, ErrNotFound
	}
	return f, nil
}

// RunScheduledBackup writes one backup into the folder and prunes old ones.
func RunScheduledBackup(ctx context.Context, b BackupSchedule) (string, error) {
	if err := os.MkdirAll(b.Dir, 0o700); err != nil {
		return "", err
	}
	name := "ordryn-backup-" + time.Now().UTC().Format("20060102-150405") + ".tar.gz"
	final := filepath.Join(b.Dir, name)
	part := final + ".part"
	f, err := os.OpenFile(part, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return "", err
	}
	if err := WriteBackup(ctx, f); err != nil {
		f.Close()
		os.Remove(part)
		return "", err
	}
	if err := f.Close(); err != nil {
		os.Remove(part)
		return "", err
	}
	if err := os.Rename(part, final); err != nil {
		return "", err
	}
	files, err := ListBackupFiles(b.Dir)
	if err == nil {
		for i, old := range files {
			if i >= b.Keep {
				_ = os.Remove(filepath.Join(b.Dir, old.Name))
			}
		}
	}
	return name, nil
}

var backupWorkerOnce sync.Once

// StartBackupWorker runs scheduled backups when BACKUP_DIR and
// BACKUP_INTERVAL_HOURS are set. It waits for the interval after the newest
// existing backup, so restarts don't pile up extra copies.
func StartBackupWorker() {
	b := LoadBackupSchedule()
	if !b.Enabled() {
		return
	}
	backupWorkerOnce.Do(func() {
		go func() {
			interval := time.Duration(b.IntervalHours) * time.Hour
			for {
				wait := time.Minute
				if files, _ := ListBackupFiles(b.Dir); len(files) > 0 {
					if next := files[0].CreatedAt.Add(interval); time.Until(next) > wait {
						wait = time.Until(next)
					}
				}
				time.Sleep(wait)
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
				if name, err := RunScheduledBackup(ctx, b); err != nil {
					fmt.Printf("backup: %v\n", err)
				} else {
					fmt.Printf("backup: wrote %s\n", name)
				}
				cancel()
			}
		}()
	})
}
