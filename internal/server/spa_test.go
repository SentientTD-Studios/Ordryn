package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestSpaRootRedirect(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	spaRootRedirect(rec, req)
	if rec.Code != http.StatusTemporaryRedirect {
		t.Fatalf("status = %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/app/" {
		t.Fatalf("Location = %q", loc)
	}
}

func TestServeSPAFallbackToIndex(t *testing.T) {
	dir := t.TempDir()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })

	if err := os.MkdirAll("web/dist/assets", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("web/dist/index.html", []byte("<html>spa</html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("web/dist/assets/app.js", []byte("console.log(1)"), 0o644); err != nil {
		t.Fatal(err)
	}

	fs := http.StripPrefix("/app/", http.FileServer(http.Dir("web/dist")))

	req := httptest.NewRequest(http.MethodGet, "/app/tasks/1", nil)
	rec := httptest.NewRecorder()
	serveSPA(rec, req, "/app", fs)
	if rec.Code != http.StatusOK {
		t.Fatalf("fallback status = %d", rec.Code)
	}
	if body := rec.Body.String(); body != "<html>spa</html>" {
		t.Fatalf("fallback body = %q", body)
	}

	req = httptest.NewRequest(http.MethodGet, "/app/assets/app.js", nil)
	rec = httptest.NewRecorder()
	serveSPA(rec, req, "/app", fs)
	if rec.Code != http.StatusOK {
		t.Fatalf("asset status = %d", rec.Code)
	}
	if body := rec.Body.String(); body != "console.log(1)" {
		t.Fatalf("asset body = %q", body)
	}
}
