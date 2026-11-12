package handlers

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"GoTodo/internal/domain"
	"GoTodo/internal/server/utils"
)

// APIV1AdminBackups handles (admins only):
//
//	GET /api/v2/admin/backups                 status, schedule, stored files
//	GET /api/v2/admin/backups/download        a fresh backup, streamed
//	GET /api/v2/admin/backups/files/{name}    a stored backup
func APIV1AdminBackups(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		return
	}
	sched := domain.LoadBackupSchedule()
	sub := utils.ParseAPIV1Subpath(r, "admin/backups")
	switch {
	case sub == "":
		files, err := domain.ListBackupFiles(sched.Dir)
		if err != nil {
			utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Could not read the backup folder.")
			return
		}
		v := domain.PGDumpVersion(r.Context())
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"pg_dump_available": v != "", "pg_dump_version": v,
			"schedule": sched, "scheduled": sched.Enabled(), "files": files,
		})
	case sub == "download":
		name := "ordryn-backup-" + time.Now().UTC().Format("20060102-150405") + ".tar.gz"
		// Probe first so a missing pg_dump is a clean JSON error, not a broken download.
		if domain.PGDumpVersion(r.Context()) == "" {
			utils.APIJSONError(w, http.StatusServiceUnavailable, "pg_dump_missing",
				"pg_dump isn't installed on the server. Install the PostgreSQL client tools or set PG_DUMP_PATH.")
			return
		}
		w.Header().Set("Content-Type", "application/gzip")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, name))
		if err := domain.WriteBackup(r.Context(), w); err != nil {
			// Headers are gone; the truncated archive fails to open, and the log says why.
			fmt.Printf("backup download: %v\n", err)
			return
		}
		logAdminEvent(r, "backup_downloaded", "site", 1, name, nil)
	case strings.HasPrefix(sub, "files/"):
		f, err := domain.OpenBackupFile(sched.Dir, strings.TrimPrefix(sub, "files/"))
		if err != nil {
			if errors.Is(err, domain.ErrNotFound) {
				utils.APIJSONError(w, http.StatusNotFound, "not_found", "Backup not found.")
				return
			}
			utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Could not open the backup.")
			return
		}
		defer f.Close()
		name := strings.TrimPrefix(sub, "files/")
		w.Header().Set("Content-Type", "application/gzip")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, name))
		_, _ = io.Copy(w, f)
		logAdminEvent(r, "backup_downloaded", "site", 1, name, nil)
	default:
		utils.APIJSONError(w, http.StatusNotFound, "not_found", "Not found.")
	}
}
