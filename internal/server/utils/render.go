package utils

import (
	"fmt"
	"html/template"
	"net/http"
	"os"
	"strings"

	"GoTodo/internal/config"
	"GoTodo/internal/sessionstore"
	"GoTodo/internal/storage"
	"GoTodo/internal/version"
)

var Templates *template.Template
var BasePath string

func InitializeTemplates() error {
	var err error
	// Load repo config (fallbacks to env/defaults internally)
	config.Load()
	BasePath = config.Cfg.BasePath
	if BasePath == "" {
		BasePath = "/"
	}

	BasePath = strings.TrimSuffix(BasePath, "/")
	if BasePath == "" {
		BasePath = "/"
	}

	Templates, err = template.New("").Funcs(template.FuncMap{
		"safeHTML": func(s string) template.HTML {
			return template.HTML(s)
		},
		"cspNonce": func(r *http.Request) string {
			return GetCSPNonce(r)
		},
		"hasPermission": func(permissions []string, permission string) bool {
			for _, p := range permissions {
				if p == permission {
					return true
				}
			}
			return false
		},
		"basePath": func() string {
			return GetBasePath()
		},
		"themeIs": func(theme interface{}, want string) bool {
			return fmt.Sprintf("%v", theme) == want
		},
		"dict": func(values ...interface{}) (map[string]interface{}, error) {
			if len(values)%2 != 0 {
				return nil, fmt.Errorf("dict requires an even number of arguments")
			}
			dict := make(map[string]interface{}, len(values)/2)
			for i := 0; i < len(values); i += 2 {
				key, ok := values[i].(string)
				if !ok {
					return nil, fmt.Errorf("dict keys must be strings")
				}
				dict[key] = values[i+1]
			}
			return dict, nil
		},
		"dueDateClass": DueDateClass,
	}).ParseGlob("internal/server/templates/*.html")
	if err != nil {
		return err
	}
	_, err = Templates.ParseGlob("internal/server/templates/partials/*.html")
	return err
}

// RenderTemplate renders templates and injects AssetVersion and optional theme from cookie.
func RenderTemplate(w http.ResponseWriter, r *http.Request, tmpl string, data interface{}) error {
	envAsset := os.Getenv("ASSET_VERSION")
	fileAsset := ""
	if b, err := os.ReadFile("internal/server/public/.asset_version"); err == nil {
		if v := strings.TrimSpace(string(b)); v != "" {
			fileAsset = v
		}
	}

	// assetVersion preference: env -> .asset_version file -> config -> default
	assetVersion := envAsset
	if assetVersion == "" {
		assetVersion = fileAsset
	}
	if assetVersion == "" {
		if config.Cfg.AssetVersion != "" {
			assetVersion = config.Cfg.AssetVersion
		} else {
			assetVersion = "20251130"
		}
	}

	// Use minified assets only when an explicit env or .asset_version file is present
	// and the minified files actually exist next to the sources. This prevents
	// accidentally serving .min files on dev if they were committed or missing.
	useMinified := false
	if envAsset != "" {
		useMinified = true
	} else if fileAsset != "" {
		// Check that both minified outputs exist before enabling.
		if _, errJs := os.Stat("internal/server/public/js/site.min.js"); errJs == nil {
			if _, errCss := os.Stat("internal/server/public/css/site.min.css"); errCss == nil {
				useMinified = true
			}
		}
	}

	var execErr error
	// If data is a map, inject AssetVersion and theme
	if ctx, ok := data.(map[string]interface{}); ok {
		ctx["AssetVersion"] = assetVersion
		ctx["UseMinifiedAssets"] = useMinified
		ctx["CSPNonce"] = GetCSPNonce(r)
		// Inject site config values. Prefer DB-backed settings when available.
		ctx["SiteName"] = config.Cfg.SiteName
		ctx["DefaultTimezone"] = config.Cfg.DefaultTimezone
		ctx["ShowChangelog"] = config.Cfg.ShowChangelog
		ctx["EnableRegistration"] = true
		ctx["InviteOnly"] = true
		ctx["MetaDescription"] = ""
		ctx["EnableGlobalAnnouncement"] = false
		ctx["GlobalAnnouncementText"] = ""
		// Site version comes only from the baked-in binary; never from DB
		ctx["SiteVersion"] = version.Version
		if s, err := storage.GetSiteSettings(); err == nil && s != nil {
			if s.SiteName != "" {
				ctx["SiteName"] = s.SiteName
			}
			if s.DefaultTimezone != "" {
				ctx["DefaultTimezone"] = s.DefaultTimezone
			}
			ctx["ShowChangelog"] = s.ShowChangelog
			ctx["EnableRegistration"] = s.EnableRegistration
			ctx["InviteOnly"] = s.InviteOnly
			ctx["MetaDescription"] = s.MetaDescription
			ctx["EnableGlobalAnnouncement"] = s.EnableGlobalAnnouncement
			ctx["GlobalAnnouncementText"] = s.GlobalAnnouncementText
		}

		if r != nil {
			session, err := sessionstore.Store.Get(r, "session")
			if err == nil && session != nil {
				if dismissed, ok := session.Values["announcement_dismissed"].(bool); ok && dismissed {
					// Override the DB setting - hide announcement for this session
					ctx["EnableGlobalAnnouncement"] = false
				}
			}
		}

		// Inject theme from cookie if present
		if r != nil {
			if c, err := r.Cookie("theme"); err == nil {
				ctx["Theme"] = c.Value
			}
		}
		// Inject any flash messages from session
		if r != nil {
			if sess, err := sessionstore.Store.Get(r, "session"); err == nil && sess != nil {
				if fl := sess.Flashes(); len(fl) > 0 {
					ctx["Flashes"] = fl
					_ = sess.Save(r, w)
				}
			}
		}
		execErr = Templates.ExecuteTemplate(w, tmpl, ctx)
	} else {
		ctx := map[string]interface{}{
			"Data":              data,
			"AssetVersion":      assetVersion,
			"UseMinifiedAssets": useMinified,
			"CSPNonce":          GetCSPNonce(r),
		}
		// Inject site config values. Prefer DB for mutable fields; site version is baked-in only.
		ctx["SiteName"] = config.Cfg.SiteName
		ctx["DefaultTimezone"] = config.Cfg.DefaultTimezone
		ctx["ShowChangelog"] = config.Cfg.ShowChangelog
		ctx["EnableRegistration"] = true
		ctx["InviteOnly"] = true
		ctx["MetaDescription"] = ""
		ctx["EnableGlobalAnnouncement"] = false
		ctx["GlobalAnnouncementText"] = ""
		ctx["SiteVersion"] = version.Version
		if s, err := storage.GetSiteSettings(); err == nil && s != nil {
			if s.SiteName != "" {
				ctx["SiteName"] = s.SiteName
			}
			if s.DefaultTimezone != "" {
				ctx["DefaultTimezone"] = s.DefaultTimezone
			}
			ctx["ShowChangelog"] = s.ShowChangelog
			ctx["EnableRegistration"] = s.EnableRegistration
			ctx["InviteOnly"] = s.InviteOnly
			ctx["MetaDescription"] = s.MetaDescription
			ctx["EnableGlobalAnnouncement"] = s.EnableGlobalAnnouncement
			ctx["GlobalAnnouncementText"] = s.GlobalAnnouncementText
		}
		if r != nil {
			if c, err := r.Cookie("theme"); err == nil {
				ctx["Theme"] = c.Value
			}
			if sess, err := sessionstore.Store.Get(r, "session"); err == nil && sess != nil {
				if fl := sess.Flashes(); len(fl) > 0 {
					ctx["Flashes"] = fl
					_ = sess.Save(r, w)
				}
			}
		}
		execErr = Templates.ExecuteTemplate(w, tmpl, ctx)
	}
	if execErr != nil {
		fmt.Println("Error parsing template: ", execErr)
		if w.Header().Get("Content-Type") == "" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
		}
		http.Error(w, execErr.Error(), http.StatusInternalServerError)
		return execErr
	}
	return nil
}

// GetBasePath returns the base path for use in templates
func GetBasePath() string {
	return BasePath
}
