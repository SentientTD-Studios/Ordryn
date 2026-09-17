package handlers

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"GoTodo/internal/imagehost"
	"GoTodo/internal/server/utils"
	"GoTodo/internal/storage"
)

// probeImageHosting is overridable in tests.
var probeImageHosting = imagehost.Probe

// loadSiteSettingsForImageTest is overridable in tests.
var loadSiteSettingsForImageTest = storage.GetSiteSettings

// APIV1AdminImageHostingTest handles POST /api/v2/admin/image-hosting/test.
// It uploads (and deletes) a tiny probe image using the form values, falling
// back to saved settings for any omitted secret.
func APIV1AdminImageHostingTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		return
	}
	var req adminSettingsPatch
	if r.Body != nil {
		defer r.Body.Close()
		dec := json.NewDecoder(r.Body)
		if err := dec.Decode(&req); err != nil && err != io.EOF {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON.")
			return
		}
	}
	saved, _ := loadSiteSettingsForImageTest()
	cfg, errMsg := imageHostingConfigForTest(saved, &req)
	if errMsg != "" {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", errMsg)
		return
	}
	if !cfg.Enabled() {
		utils.APIJSONError(w, http.StatusBadRequest, "not_configured", "Select S3-compatible or local uploads first.")
		return
	}
	if imagehost.NormalizeProvider(cfg.Provider) == imagehost.ProviderLocal {
		cfg.LocalPublicBase = localPublicBase(r)
	}
	result := probeImageHosting(r.Context(), cfg)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":            result.OK,
		"message":       result.Message,
		"public_url_ok": result.PublicURLOK,
	})
}

func imageHostingConfigForTest(saved *storage.SiteSettings, req *adminSettingsPatch) (imagehost.Config, string) {
	var cfg imagehost.Config
	if saved != nil {
		cfg = saved.Image
	}
	hasNewSecret := false
	if req != nil {
		if req.ImageHostingProvider != nil {
			cfg.Provider = *req.ImageHostingProvider
		}
		if req.ImageMaxBytes != nil {
			cfg.MaxBytes = *req.ImageMaxBytes
		}
		if req.ImageS3Endpoint != nil {
			cfg.S3Endpoint = strings.TrimSpace(*req.ImageS3Endpoint)
		}
		if req.ImageS3Region != nil {
			cfg.S3Region = strings.TrimSpace(*req.ImageS3Region)
		}
		if req.ImageS3Bucket != nil {
			cfg.S3Bucket = strings.TrimSpace(*req.ImageS3Bucket)
		}
		if req.ImageS3AccessKey != nil {
			cfg.S3AccessKey = strings.TrimSpace(*req.ImageS3AccessKey)
		}
		if req.ImageS3SecretKey != nil {
			if sec := strings.TrimSpace(*req.ImageS3SecretKey); sec != "" {
				cfg.S3SecretKey = sec
				hasNewSecret = true
			}
		}
		if req.ImageS3PublicURL != nil {
			cfg.S3PublicURL = strings.TrimSpace(*req.ImageS3PublicURL)
		}
		if req.ImageS3ForcePathStyle != nil {
			cfg.S3ForcePathStyle = *req.ImageS3ForcePathStyle
		}
		if req.ImageLocalPath != nil {
			cfg.LocalPath = strings.TrimSpace(*req.ImageLocalPath)
		}
	}
	if !hasNewSecret && saved != nil && strings.TrimSpace(saved.ImageS3SecretKeyEnc) != "" {
		loaded, err := saved.ImageHostingConfig()
		if err != nil {
			return cfg, "Could not load the saved secret key."
		}
		cfg.S3SecretKey = loaded.S3SecretKey
	}
	if msg := cfg.Validate(); msg != "" {
		return cfg, msg
	}
	return cfg, ""
}
