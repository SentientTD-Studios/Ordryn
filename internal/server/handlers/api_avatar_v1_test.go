package handlers

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"GoTodo/internal/domain"
	"GoTodo/internal/imagehost"
	"GoTodo/internal/server/utils"
	"GoTodo/internal/storage"
)

var (
	testTinyPNG  = mustDecodeB64("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==")
	testTinyJPEG = mustDecodeB64("/9j/4AAQSkZJRgABAQEASABIAAD/2wBDAP//////////////////////////////////////////////////////////////////////////////////////wgALCAABAAEBAREA/8QAFBABAAAAAAAAAAAAAAAAAAAAAP/aAAgBAQABPxA=")
	testTinyGIF  = mustDecodeB64("R0lGODlhAQABAIAAAAAAAP///yH5BAEAAAAALAAAAAABAAEAAAIBRAA7")
	testTinyWebP = mustDecodeB64("UklGRh4AAABXRUJQVlA4TBEAAAAvAAAAAAfQ//73v/+BiOh/AAA=")
)

func mustDecodeB64(s string) []byte {
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		panic(err)
	}
	return b
}

func mockProfileStorage(t *testing.T, profile *storage.UserProfile) {
	t.Helper()
	prevUpdate := domain.UpdateAvatarStorage
	prevGet := domain.GetUserProfileStorage

	curr := *profile
	domain.UpdateAvatarStorage = func(userID int, avatarURL string) error {
		curr.AvatarURL = avatarURL
		return nil
	}
	domain.GetUserProfileStorage = func(userID int) (*storage.UserProfile, error) {
		p := curr
		return &p, nil
	}

	t.Cleanup(func() {
		domain.UpdateAvatarStorage = prevUpdate
		domain.GetUserProfileStorage = prevGet
	})
}

func TestAPIV1MeAvatarMethodNotAllowed(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v2/me/avatar", nil)
	req = utils.SetAPIUserID(req, 1)
	rec := httptest.NewRecorder()
	APIV1MeAvatar(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d", rec.Code)
	}
}

func TestAPIV1MeAvatarUnauthorized(t *testing.T) {
	body, ctype := multipartPNG(t, "file", "avatar.png", testTinyPNG)
	req := httptest.NewRequest(http.MethodPost, "/api/v2/me/avatar", body)
	req.Header.Set("Content-Type", ctype)
	rec := httptest.NewRecorder()
	APIV1MeAvatar(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}

	reqDel := httptest.NewRequest(http.MethodDelete, "/api/v2/me/avatar", nil)
	recDel := httptest.NewRecorder()
	APIV1MeAvatar(recDel, reqDel)
	if recDel.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s", recDel.Code, recDel.Body.String())
	}
}

func TestAPIV1MeAvatarNotConfigured(t *testing.T) {
	withImageHosting(t, imagehost.Config{}, nil)
	body, ctype := multipartPNG(t, "file", "avatar.png", testTinyPNG)
	req := httptest.NewRequest(http.MethodPost, "/api/v2/me/avatar", body)
	req.Header.Set("Content-Type", ctype)
	req = utils.SetAPIUserID(req, 1)
	rec := httptest.NewRecorder()
	APIV1MeAvatar(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "not_configured") {
		t.Fatalf("body=%s", rec.Body.String())
	}
}

func TestAPIV1MeAvatarRejectsNonImage(t *testing.T) {
	dir := t.TempDir()
	withImageHosting(t, imagehost.Config{Provider: imagehost.ProviderLocal, MaxBytes: imagehost.DefaultMaxBytes, LocalPath: dir}, nil)
	body, ctype := multipartPNG(t, "file", "avatar.txt", []byte("plain text not an image"))
	req := httptest.NewRequest(http.MethodPost, "/api/v2/me/avatar", body)
	req.Header.Set("Content-Type", ctype)
	req = utils.SetAPIUserID(req, 1)
	rec := httptest.NewRecorder()
	APIV1MeAvatar(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAPIV1MeAvatarRejectsDisallowedFormats(t *testing.T) {
	dir := t.TempDir()
	withImageHosting(t, imagehost.Config{Provider: imagehost.ProviderLocal, MaxBytes: imagehost.DefaultMaxBytes, LocalPath: dir}, nil)

	// Test GIF rejection
	bodyGIF, ctypeGIF := multipartPNG(t, "file", "avatar.gif", testTinyGIF)
	reqGIF := httptest.NewRequest(http.MethodPost, "/api/v2/me/avatar", bodyGIF)
	reqGIF.Header.Set("Content-Type", ctypeGIF)
	reqGIF = utils.SetAPIUserID(reqGIF, 1)
	recGIF := httptest.NewRecorder()
	APIV1MeAvatar(recGIF, reqGIF)
	if recGIF.Code != http.StatusBadRequest {
		t.Fatalf("GIF status=%d body=%s", recGIF.Code, recGIF.Body.String())
	}
	if !strings.Contains(recGIF.Body.String(), "PNG or JPEG") {
		t.Fatalf("expected PNG or JPEG error message, got %s", recGIF.Body.String())
	}

	// Test WebP rejection
	bodyWebP, ctypeWebP := multipartPNG(t, "file", "avatar.webp", testTinyWebP)
	reqWebP := httptest.NewRequest(http.MethodPost, "/api/v2/me/avatar", bodyWebP)
	reqWebP.Header.Set("Content-Type", ctypeWebP)
	reqWebP = utils.SetAPIUserID(reqWebP, 1)
	recWebP := httptest.NewRecorder()
	APIV1MeAvatar(recWebP, reqWebP)
	if recWebP.Code != http.StatusBadRequest {
		t.Fatalf("WebP status=%d body=%s", recWebP.Code, recWebP.Body.String())
	}
	if !strings.Contains(recWebP.Body.String(), "PNG or JPEG") {
		t.Fatalf("expected PNG or JPEG error message, got %s", recWebP.Body.String())
	}
}

func TestAPIV1MeAvatarRejectsOversize(t *testing.T) {
	dir := t.TempDir()
	withImageHosting(t, imagehost.Config{Provider: imagehost.ProviderLocal, MaxBytes: imagehost.MinMaxBytes, LocalPath: dir}, nil)
	big := make([]byte, imagehost.MinMaxBytes+1)
	copy(big, testTinyPNG)
	body, ctype := multipartPNG(t, "file", "big.png", big)
	req := httptest.NewRequest(http.MethodPost, "/api/v2/me/avatar", body)
	req.Header.Set("Content-Type", ctype)
	req = utils.SetAPIUserID(req, 1)
	rec := httptest.NewRecorder()
	APIV1MeAvatar(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAPIV1MeAvatarPNGSuccess(t *testing.T) {
	dir := t.TempDir()
	withImageHosting(t, imagehost.Config{Provider: imagehost.ProviderLocal, MaxBytes: imagehost.DefaultMaxBytes, LocalPath: dir}, nil)
	mockProfileStorage(t, &storage.UserProfile{ID: 1, Email: "user@example.com", UserName: "alice"})

	body, ctype := multipartPNG(t, "file", "avatar.png", testTinyPNG)
	req := httptest.NewRequest(http.MethodPost, "/api/v2/me/avatar", body)
	req.Header.Set("Content-Type", ctype)
	req.Host = "gotodo.test"
	req = utils.SetAPIUserID(req, 1)
	rec := httptest.NewRecorder()
	APIV1MeAvatar(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}

	var user apiUserMeJSON
	if err := json.Unmarshal(rec.Body.Bytes(), &user); err != nil {
		t.Fatal(err)
	}
	if user.AvatarURL == "" || !strings.Contains(user.AvatarURL, "/uploads/") || !strings.HasSuffix(user.AvatarURL, ".png") {
		t.Fatalf("unexpected avatar_url=%q", user.AvatarURL)
	}
}

func TestAPIV1MeAvatarJPEGSuccess(t *testing.T) {
	dir := t.TempDir()
	withImageHosting(t, imagehost.Config{Provider: imagehost.ProviderLocal, MaxBytes: imagehost.DefaultMaxBytes, LocalPath: dir}, nil)
	mockProfileStorage(t, &storage.UserProfile{ID: 1, Email: "user@example.com", UserName: "alice"})

	body, ctype := multipartPNG(t, "file", "avatar.jpg", testTinyJPEG)
	req := httptest.NewRequest(http.MethodPost, "/api/v2/me/avatar", body)
	req.Header.Set("Content-Type", ctype)
	req.Host = "gotodo.test"
	req = utils.SetAPIUserID(req, 1)
	rec := httptest.NewRecorder()
	APIV1MeAvatar(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}

	var user apiUserMeJSON
	if err := json.Unmarshal(rec.Body.Bytes(), &user); err != nil {
		t.Fatal(err)
	}
	if user.AvatarURL == "" || !strings.Contains(user.AvatarURL, "/uploads/") || !strings.HasSuffix(user.AvatarURL, ".jpg") {
		t.Fatalf("unexpected avatar_url=%q", user.AvatarURL)
	}
}

func TestAPIV1MeAvatarDeleteSuccess(t *testing.T) {
	mockProfileStorage(t, &storage.UserProfile{ID: 1, Email: "user@example.com", UserName: "alice", AvatarURL: "/uploads/old.png"})

	req := httptest.NewRequest(http.MethodDelete, "/api/v2/me/avatar", nil)
	req = utils.SetAPIUserID(req, 1)
	rec := httptest.NewRecorder()
	APIV1MeAvatar(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}

	var user apiUserMeJSON
	if err := json.Unmarshal(rec.Body.Bytes(), &user); err != nil {
		t.Fatal(err)
	}
	if user.AvatarURL != "" {
		t.Fatalf("expected avatar_url to be empty, got %q", user.AvatarURL)
	}
}
