package extensions

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const defaultDir = "data/extensions"

var (
	mu      sync.RWMutex
	entries []Entry
)

// Entry is one scanned extension folder, loaded or failed.
type Entry struct {
	ID       string   `json:"id"`
	Dir      string   `json:"-"`
	Manifest Manifest `json:"manifest"`
	Error    string   `json:"error,omitempty"`
	Loaded   bool     `json:"loaded"`
}

// Dir returns the extensions root (EXTENSIONS_DIR, MODS_DIR, data/extensions, or data/mods).
func Dir() string {
	if v := strings.TrimSpace(os.Getenv("EXTENSIONS_DIR")); v != "" {
		return v
	}
	if v := strings.TrimSpace(os.Getenv("MODS_DIR")); v != "" {
		return v
	}
	if isDir(defaultDir) {
		return defaultDir
	}
	if isDir("data/mods") {
		return "data/mods"
	}
	return defaultDir
}

func isDir(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.IsDir()
}

// Load scans the extensions directory. Failures disable that extension only.
func Load() []Entry {
	root := Dir()
	loaded := scan(root)
	mu.Lock()
	entries = loaded
	mu.Unlock()
	for _, e := range loaded {
		if e.Loaded {
			fmt.Printf("extensions: loaded %s (%s)\n", e.ID, e.Manifest.Version)
		} else if e.Error != "" {
			fmt.Printf("extensions: failed %s: %s\n", e.ID, e.Error)
		}
	}
	return Snapshot()
}

func scan(root string) []Entry {
	info, err := os.Stat(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		fmt.Printf("extensions: cannot read %s: %v\n", root, err)
		return nil
	}
	if !info.IsDir() {
		fmt.Printf("extensions: %s is not a directory\n", root)
		return nil
	}
	children, err := os.ReadDir(root)
	if err != nil {
		fmt.Printf("extensions: cannot list %s: %v\n", root, err)
		return nil
	}
	out := make([]Entry, 0)
	seen := make(map[string]int)
	for _, child := range children {
		if !child.IsDir() {
			continue
		}
		folder := child.Name()
		if strings.HasPrefix(folder, ".") {
			continue
		}
		e := loadFolder(root, folder)
		if e.Error == "missing manifest.json" && !e.Loaded {
			fmt.Printf("extensions: skipped %s (no manifest.json)\n", folder)
			continue
		}
		if prev, ok := seen[e.ID]; ok && e.ID != "" {
			e.Loaded = false
			e.Error = fmt.Sprintf("duplicate id %q (also %s)", e.ID, out[prev].Dir)
			out[prev].Loaded = false
			if out[prev].Error == "" {
				out[prev].Error = fmt.Sprintf("duplicate id %q", e.ID)
			}
		} else if e.ID != "" {
			seen[e.ID] = len(out)
		}
		out = append(out, e)
	}
	return out
}

func loadFolder(root, folder string) Entry {
	dir := filepath.Join(root, folder)
	e := Entry{ID: folder, Dir: dir}
	raw, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		if os.IsNotExist(err) {
			e.Error = "missing manifest.json"
			return e
		}
		e.Error = fmt.Sprintf("read manifest.json: %v", err)
		return e
	}
	var m Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		e.Error = fmt.Sprintf("invalid manifest.json: %v", err)
		return e
	}
	e.ID = strings.TrimSpace(m.ID)
	if e.ID == "" {
		e.ID = folder
	}
	if err := ValidateManifest(folder, m); err != nil {
		e.Error = err.Error()
		e.Manifest = m
		return e
	}
	if ui := strings.TrimSpace(m.UI); ui != "" {
		uiPath := filepath.Join(dir, filepath.Clean(ui))
		if !strings.HasPrefix(uiPath, dir+string(os.PathSeparator)) && uiPath != dir {
			e.Error = "ui path is outside the extension folder"
			e.Manifest = m
			return e
		}
		if st, err := os.Stat(uiPath); err != nil || st.IsDir() {
			e.Error = fmt.Sprintf("ui file %q is missing", ui)
			e.Manifest = m
			return e
		}
	}
	e.Manifest = m
	e.Loaded = true
	return e
}

// Snapshot returns a copy of the current registry.
func Snapshot() []Entry {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]Entry, len(entries))
	copy(out, entries)
	return out
}

// Get returns a loaded or failed entry by id.
func Get(id string) (Entry, bool) {
	mu.RLock()
	defer mu.RUnlock()
	for _, e := range entries {
		if e.ID == id {
			return e, true
		}
	}
	return Entry{}, false
}

// LoadedCount is the number of successfully loaded extensions.
func LoadedCount() int {
	mu.RLock()
	defer mu.RUnlock()
	n := 0
	for _, e := range entries {
		if e.Loaded {
			n++
		}
	}
	return n
}

// FieldExtensionIDs returns loaded extension ids that register custom fields.
func FieldExtensionIDs() []string {
	out := make([]string, 0)
	for _, e := range LoadedEntries() {
		if e.Manifest.HasFields() {
			out = append(out, e.ID)
		}
	}
	return out
}

// LoadedEntries returns only successfully loaded extensions.
func LoadedEntries() []Entry {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]Entry, 0)
	for _, e := range entries {
		if e.Loaded {
			out = append(out, e)
		}
	}
	return out
}
