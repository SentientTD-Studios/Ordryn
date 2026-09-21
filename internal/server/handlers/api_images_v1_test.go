package handlers

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"GoTodo/internal/imagehost"
	"GoTodo/internal/server/utils"
)

var handlerTinyPNG = mustPNG()

func mustPNG() []byte {
	b, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==")
	if err != nil {
		panic(err)
	}
	return b
}

func withImageHosting(t *testing.T, cfg imagehost.Config, store imagehost.Store) {
	t.Helper()
	prevLoad, prevOpen := loadImageHosting, openImageStore
	loadImageHosting = func() (imagehost.Config, error) { return cfg, nil }
	if store != nil {
		openImageStore = func(imagehost.Config) (imagehost.Store, error) { return store, nil }
	} else {
		openImageStore = imagehost.NewStore
	}
	t.Cleanup(func() {
		loadImageHosting = prevLoad
		openImageStore = prevOpen
	})
}

func multipartPNG(t *testing.T, field, filename string, data []byte) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, err := w.CreateFormFile(field, filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return &buf, w.FormDataContentType()
}

func TestAPIV1ImagesMethodNotAllowed(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v2/images", nil)
	req = utils.SetAPIUserID(req, 1)
	rec := httptest.NewRecorder()
	APIV1Images(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d", rec.Code)
	}
}

func TestAPIV1ImagesUnauthorized(t *testing.T) {
	body, ctype := multipartPNG(t, "file", "dot.png", handlerTinyPNG)
	req := httptest.NewRequest(http.MethodPost, "/api/v2/images", body)
	req.Header.Set("Content-Type", ctype)
	rec := httptest.NewRecorder()
	APIV1Images(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAPIV1ImagesNotConfigured(t *testing.T) {
	withImageHosting(t, imagehost.Config{}, nil)
	body, ctype := multipartPNG(t, "file", "dot.png", handlerTinyPNG)
	req := httptest.NewRequest(http.MethodPost, "/api/v2/images", body)
	req.Header.Set("Content-Type", ctype)
	req = utils.SetAPIUserID(req, 1)
	rec := httptest.NewRecorder()
	APIV1Images(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "not_configured") {
		t.Fatalf("body=%s", rec.Body.String())
	}
}

func TestAPIV1ImagesRejectsNonImage(t *testing.T) {
	dir := t.TempDir()
	withImageHosting(t, imagehost.Config{Provider: imagehost.ProviderLocal, MaxBytes: imagehost.DefaultMaxBytes, LocalPath: dir}, nil)
	body, ctype := multipartPNG(t, "file", "notes.txt", []byte("hello world this is not an image"))
	req := httptest.NewRequest(http.MethodPost, "/api/v2/images", body)
	req.Header.Set("Content-Type", ctype)
	req = utils.SetAPIUserID(req, 1)
	rec := httptest.NewRecorder()
	APIV1Images(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAPIV1ImagesRejectsOversize(t *testing.T) {
	dir := t.TempDir()
	withImageHosting(t, imagehost.Config{Provider: imagehost.ProviderLocal, MaxBytes: imagehost.MinMaxBytes, LocalPath: dir}, nil)
	big := make([]byte, imagehost.MinMaxBytes+1)
	copy(big, handlerTinyPNG)
	body, ctype := multipartPNG(t, "file", "big.png", big)
	req := httptest.NewRequest(http.MethodPost, "/api/v2/images", body)
	req.Header.Set("Content-Type", ctype)
	req = utils.SetAPIUserID(req, 1)
	rec := httptest.NewRecorder()
	APIV1Images(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAPIV1ImagesLocalUpload(t *testing.T) {
	dir := t.TempDir()
	withImageHosting(t, imagehost.Config{Provider: imagehost.ProviderLocal, MaxBytes: imagehost.DefaultMaxBytes, LocalPath: dir}, nil)
	body, ctype := multipartPNG(t, "file", "dot.png", handlerTinyPNG)
	req := httptest.NewRequest(http.MethodPost, "/api/v2/images", body)
	req.Header.Set("Content-Type", ctype)
	req.Host = "gotodo.test"
	req = utils.SetAPIUserID(req, 7)
	rec := httptest.NewRecorder()
	APIV1Images(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var out struct {
		URL         string `json:"url"`
		ContentType string `json:"content_type"`
		Size        int    `json:"size"`
		Key         string `json:"key"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.ContentType != imagehost.TypePNG {
		t.Fatalf("content_type=%q", out.ContentType)
	}
	if out.Size != len(handlerTinyPNG) {
		t.Fatalf("size=%d", out.Size)
	}
	if !strings.Contains(out.URL, "/uploads/") || !strings.HasSuffix(out.URL, out.Key) {
		t.Fatalf("url=%q key=%q", out.URL, out.Key)
	}
	if _, err := os.Stat(filepath.Join(dir, out.Key)); err != nil {
		t.Fatal(err)
	}
}

func TestServeLocalImage(t *testing.T) {
	dir := t.TempDir()
	store, err := imagehost.NewLocalStore(dir, "/uploads")
	if err != nil {
		t.Fatal(err)
	}
	obj := imagehost.Object{Key: "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee.png", ContentType: imagehost.TypePNG, Data: handlerTinyPNG}
	if _, err := store.Put(context.Background(), obj); err != nil {
		t.Fatal(err)
	}
	withImageHosting(t, imagehost.Config{Provider: imagehost.ProviderLocal, LocalPath: dir}, store)

	req := httptest.NewRequest(http.MethodGet, "/uploads/"+obj.Key, nil)
	rec := httptest.NewRecorder()
	ServeLocalImage(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d", rec.Code)
	}
	if rec.Header().Get("Content-Type") != imagehost.TypePNG {
		t.Fatalf("content-type=%q", rec.Header().Get("Content-Type"))
	}
	got, _ := io.ReadAll(rec.Body)
	if !bytes.Equal(got, handlerTinyPNG) {
		t.Fatal("body mismatch")
	}

	req = httptest.NewRequest(http.MethodGet, "/uploads/../secret.png", nil)
	rec = httptest.NewRecorder()
	ServeLocalImage(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("traversal status=%d", rec.Code)
	}
}

type stubStore struct {
	url string
	err error
	obj imagehost.Object
}

func (s *stubStore) Put(ctx context.Context, obj imagehost.Object) (string, error) {
	s.obj = obj
	return s.url, s.err
}

func TestAPIV1ImagesS3Upload(t *testing.T) {
	stub := &stubStore{url: "https://cdn.example.com/aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee.png"}
	cfg := imagehost.Config{
		Provider:         imagehost.ProviderS3,
		MaxBytes:         imagehost.DefaultMaxBytes,
		S3Endpoint:       "https://nyc3.digitaloceanspaces.com",
		S3Region:         "nyc3",
		S3Bucket:         "media",
		S3AccessKey:      "key",
		S3SecretKey:      "secret",
		S3PublicURL:      "https://cdn.example.com",
		S3ForcePathStyle: true,
	}
	withImageHosting(t, cfg, stub)
	body, ctype := multipartPNG(t, "file", "dot.png", handlerTinyPNG)
	req := httptest.NewRequest(http.MethodPost, "/api/v2/images", body)
	req.Header.Set("Content-Type", ctype)
	req = utils.SetAPIUserID(req, 1)
	rec := httptest.NewRecorder()
	APIV1Images(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if stub.obj.ContentType != imagehost.TypePNG {
		t.Fatalf("stored type=%q", stub.obj.ContentType)
	}
	var out map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out["url"] != stub.url {
		t.Fatalf("url=%v", out["url"])
	}
}

func TestAPIV1ImagesStoreClientError(t *testing.T) {
	stub := &stubStore{err: &imagehost.UploadError{Status: 403, Code: "AccessDenied", Message: "Access Denied"}}
	cfg := imagehost.Config{
		Provider:         imagehost.ProviderS3,
		MaxBytes:         imagehost.DefaultMaxBytes,
		S3Endpoint:       "https://abc.r2.cloudflarestorage.com",
		S3Region:         "auto",
		S3Bucket:         "media",
		S3AccessKey:      "key",
		S3SecretKey:      "secret",
		S3PublicURL:      "https://cdn.example.com",
		S3ForcePathStyle: true,
	}
	withImageHosting(t, cfg, stub)
	body, ctype := multipartPNG(t, "file", "dot.png", handlerTinyPNG)
	req := httptest.NewRequest(http.MethodPost, "/api/v2/images", body)
	req.Header.Set("Content-Type", ctype)
	req = utils.SetAPIUserID(req, 1)
	rec := httptest.NewRecorder()
	APIV1Images(rec, req)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "AccessDenied") {
		t.Fatalf("leaked provider code: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "site admin") {
		t.Fatalf("body=%s", rec.Body.String())
	}
}

func TestAPIV1ImagesStoreGatewayError(t *testing.T) {
	stub := &stubStore{err: &imagehost.UploadError{Status: 502, Message: "the storage provider returned an HTML error page (often HTTP/2 or a gateway failure)"}}
	cfg := imagehost.Config{
		Provider:         imagehost.ProviderS3,
		MaxBytes:         imagehost.DefaultMaxBytes,
		S3Endpoint:       "https://abc.r2.cloudflarestorage.com",
		S3Region:         "auto",
		S3Bucket:         "media",
		S3AccessKey:      "key",
		S3SecretKey:      "secret",
		S3PublicURL:      "https://cdn.example.com",
		S3ForcePathStyle: true,
	}
	withImageHosting(t, cfg, stub)
	body, ctype := multipartPNG(t, "file", "dot.png", handlerTinyPNG)
	req := httptest.NewRequest(http.MethodPost, "/api/v2/images", body)
	req.Header.Set("Content-Type", ctype)
	req = utils.SetAPIUserID(req, 1)
	rec := httptest.NewRecorder()
	APIV1Images(rec, req)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "HTML") || strings.Contains(rec.Body.String(), "<html") {
		t.Fatalf("leaked html: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "temporarily unavailable") {
		t.Fatalf("body=%s", rec.Body.String())
	}
}

func TestAPIV1ImagesMissingFile(t *testing.T) {
	dir := t.TempDir()
	withImageHosting(t, imagehost.Config{Provider: imagehost.ProviderLocal, LocalPath: dir, MaxBytes: imagehost.DefaultMaxBytes}, nil)
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	_ = w.WriteField("name", "nope")
	_ = w.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/v2/images", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	req = utils.SetAPIUserID(req, 1)
	rec := httptest.NewRecorder()
	APIV1Images(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestLocalUploadKey(t *testing.T) {
	orig := utils.BasePath
	t.Cleanup(func() { utils.BasePath = orig })
	utils.BasePath = "/gotodo"
	if got := localUploadKey("/gotodo/uploads/a.png"); got != "a.png" {
		t.Fatalf("got %q", got)
	}
	utils.BasePath = "/"
	if got := localUploadKey("/uploads/a.png"); got != "a.png" {
		t.Fatalf("got %q", got)
	}
}
