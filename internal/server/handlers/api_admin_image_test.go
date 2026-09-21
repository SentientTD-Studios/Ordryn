package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"GoTodo/internal/imagehost"
	"GoTodo/internal/storage"
)

func TestValidateImageHostingSettings(t *testing.T) {
	t.Run("disabled", func(t *testing.T) {
		s := &storage.SiteSettings{}
		if msg := validateImageHostingSettings(s); msg != "" {
			t.Fatalf("msg=%q", msg)
		}
	})
	t.Run("local", func(t *testing.T) {
		s := &storage.SiteSettings{Image: imagehost.Config{Provider: "local"}}
		if msg := validateImageHostingSettings(s); msg != "" {
			t.Fatalf("msg=%q", msg)
		}
		if s.Image.LocalPath != imagehost.DefaultLocalPath {
			t.Fatalf("path=%q", s.Image.LocalPath)
		}
	})
	t.Run("s3 missing endpoint", func(t *testing.T) {
		s := &storage.SiteSettings{Image: imagehost.Config{Provider: "s3"}}
		if msg := validateImageHostingSettings(s); !strings.Contains(msg, "endpoint") {
			t.Fatalf("msg=%q", msg)
		}
	})
	t.Run("s3 complete with stored secret", func(t *testing.T) {
		s := &storage.SiteSettings{
			Image: imagehost.Config{
				Provider:    "s3",
				S3Endpoint:  "https://abc123.r2.cloudflarestorage.com",
				S3Region:    "auto",
				S3Bucket:    "media",
				S3AccessKey: "ak",
				S3PublicURL: "https://cdn.example.com",
			},
			ImageS3SecretKeyEnc: "ciphertext",
		}
		if msg := validateImageHostingSettings(s); msg != "" {
			t.Fatalf("msg=%q", msg)
		}
	})
	t.Run("s3 r2 dashboard endpoint with stored secret", func(t *testing.T) {
		s := &storage.SiteSettings{
			Image: imagehost.Config{
				Provider:         "s3",
				S3Endpoint:       "https://abc123.r2.cloudflarestorage.com/ordryn-testing",
				S3Region:         "ENAM",
				S3Bucket:         "ordryn-testing",
				S3AccessKey:      "ak",
				S3PublicURL:      "https://pub-example.r2.dev",
				S3ForcePathStyle: false,
			},
			ImageS3SecretKeyEnc: "ciphertext",
		}
		if msg := validateImageHostingSettings(s); msg != "" {
			t.Fatalf("msg=%q", msg)
		}
		if s.Image.S3Endpoint != "https://abc123.r2.cloudflarestorage.com/ordryn-testing" {
			t.Fatalf("endpoint=%q, want stored as entered", s.Image.S3Endpoint)
		}
		if s.Image.S3Region != "ENAM" {
			t.Fatalf("region=%q, want stored as entered", s.Image.S3Region)
		}
		if s.Image.S3ForcePathStyle {
			t.Fatal("path-style must stay unchecked")
		}
	})
	t.Run("s3 missing secret", func(t *testing.T) {
		s := &storage.SiteSettings{
			Image: imagehost.Config{
				Provider:    "s3",
				S3Endpoint:  "https://nyc3.digitaloceanspaces.com",
				S3Region:    "nyc3",
				S3Bucket:    "media",
				S3AccessKey: "ak",
				S3PublicURL: "https://cdn.example.com",
			},
		}
		if msg := validateImageHostingSettings(s); !strings.Contains(msg, "secret") {
			t.Fatalf("msg=%q", msg)
		}
	})
}

func TestAdminSettingsJSONIncludesImageHosting(t *testing.T) {
	raw, err := json.Marshal(adminSettingsJSON{
		SiteName:              "Demo",
		ImageHostingProvider:  imagehost.ProviderS3,
		ImageMaxBytes:         imagehost.DefaultMaxBytes,
		ImageS3Endpoint:       "https://nyc3.digitaloceanspaces.com",
		ImageS3ForcePathStyle: true,
		ImageS3SecretKeySet:   true,
	})
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{
		"enable_inbound_webhooks",
		"image_hosting_provider",
		"image_max_bytes",
		"image_s3_endpoint",
		"image_s3_region",
		"image_s3_bucket",
		"image_s3_access_key",
		"image_s3_secret_key_set",
		"image_s3_public_url",
		"image_s3_force_path_style",
		"image_local_path",
	} {
		if _, ok := m[key]; !ok {
			t.Fatalf("missing key %q in %s", key, string(raw))
		}
	}
	if _, ok := m["image_s3_secret_key"]; ok {
		t.Fatal("secret key must not be serialized on GET")
	}
}

func TestAPIV1AdminSettingsMethodNotAllowed(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/v2/admin/settings", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	APIV1AdminSettings(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d", rec.Code)
	}
}

func TestAdminSettingsPatchDecodesUncheckedPathStyleAndEndpoint(t *testing.T) {
	body := `{"image_s3_endpoint":"https://s3.example.com/changed","image_s3_region":"us-east-1","image_s3_force_path_style":false}`
	var req adminSettingsPatch
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		t.Fatal(err)
	}
	if req.ImageS3ForcePathStyle == nil || *req.ImageS3ForcePathStyle {
		t.Fatal("unchecked path-style must decode as false, not omitted")
	}
	if req.ImageS3Endpoint == nil || *req.ImageS3Endpoint != "https://s3.example.com/changed" {
		t.Fatalf("endpoint=%v", req.ImageS3Endpoint)
	}
	if req.ImageS3Region == nil || *req.ImageS3Region != "us-east-1" {
		t.Fatalf("region=%v", req.ImageS3Region)
	}
}
