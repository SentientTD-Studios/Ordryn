package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"GoTodo/internal/imagehost"
	"GoTodo/internal/storage"
)

func TestAPIV1AdminImageHostingTestMethodNotAllowed(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v2/admin/image-hosting/test", nil)
	rec := httptest.NewRecorder()
	APIV1AdminImageHostingTest(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d", rec.Code)
	}
}

func TestAPIV1AdminImageHostingTestNotConfigured(t *testing.T) {
	prevLoad, prevProbe := loadSiteSettingsForImageTest, probeImageHosting
	t.Cleanup(func() {
		loadSiteSettingsForImageTest = prevLoad
		probeImageHosting = prevProbe
	})
	loadSiteSettingsForImageTest = func() (*storage.SiteSettings, error) {
		return &storage.SiteSettings{}, nil
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v2/admin/image-hosting/test", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	APIV1AdminImageHostingTest(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAPIV1AdminImageHostingTestSuccess(t *testing.T) {
	prevLoad, prevProbe := loadSiteSettingsForImageTest, probeImageHosting
	t.Cleanup(func() {
		loadSiteSettingsForImageTest = prevLoad
		probeImageHosting = prevProbe
	})
	loadSiteSettingsForImageTest = func() (*storage.SiteSettings, error) {
		return &storage.SiteSettings{
			Image: imagehost.Config{
				Provider:    imagehost.ProviderS3,
				S3Endpoint:  "https://abc.r2.cloudflarestorage.com",
				S3Region:    "auto",
				S3Bucket:    "media",
				S3AccessKey: "ak",
				S3PublicURL: "https://cdn.example.com",
			},
			ImageS3SecretKeyEnc: "",
		}, nil
	}
	var gotCfg imagehost.Config
	probeImageHosting = func(ctx context.Context, cfg imagehost.Config) imagehost.ProbeResult {
		gotCfg = cfg
		return imagehost.ProbeResult{OK: true, PublicURLOK: true, Message: "Connected. A test image was uploaded and removed."}
	}
	body := `{"image_hosting_provider":"s3","image_s3_endpoint":"https://abc.r2.cloudflarestorage.com","image_s3_region":"auto","image_s3_bucket":"media","image_s3_access_key":"ak","image_s3_secret_key":"supersecret","image_s3_public_url":"https://cdn.example.com","image_s3_force_path_style":true}`
	req := httptest.NewRequest(http.MethodPost, "/api/v2/admin/image-hosting/test", strings.NewReader(body))
	rec := httptest.NewRecorder()
	APIV1AdminImageHostingTest(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var out map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out["ok"] != true {
		t.Fatalf("out=%v", out)
	}
	if gotCfg.S3SecretKey != "supersecret" {
		t.Fatalf("secret not passed to probe")
	}
	if strings.Contains(rec.Body.String(), "supersecret") {
		t.Fatal("secret leaked in response")
	}
}

func TestAPIV1AdminImageHostingTestFailure(t *testing.T) {
	prevLoad, prevProbe := loadSiteSettingsForImageTest, probeImageHosting
	t.Cleanup(func() {
		loadSiteSettingsForImageTest = prevLoad
		probeImageHosting = prevProbe
	})
	loadSiteSettingsForImageTest = func() (*storage.SiteSettings, error) {
		return &storage.SiteSettings{}, nil
	}
	probeImageHosting = func(ctx context.Context, cfg imagehost.Config) imagehost.ProbeResult {
		return imagehost.ProbeResult{
			OK:      false,
			Message: (&imagehost.UploadError{Status: 403, Code: "AccessDenied", Message: "Access Denied"}).AdminMessage(),
		}
	}
	body := `{"image_hosting_provider":"s3","image_s3_endpoint":"https://abc.r2.cloudflarestorage.com","image_s3_region":"auto","image_s3_bucket":"media","image_s3_access_key":"ak","image_s3_secret_key":"s","image_s3_public_url":"https://cdn.example.com"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v2/admin/image-hosting/test", bytes.NewReader([]byte(body)))
	rec := httptest.NewRecorder()
	APIV1AdminImageHostingTest(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "<?xml") {
		t.Fatal("xml leaked")
	}
	if !strings.Contains(rec.Body.String(), `"ok":false`) && !strings.Contains(rec.Body.String(), `"ok": false`) {
		t.Fatalf("body=%s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "AccessDenied") {
		t.Fatalf("admin should see code: %s", rec.Body.String())
	}
}
