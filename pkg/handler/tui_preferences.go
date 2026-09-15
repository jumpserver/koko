package handler

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"

	"github.com/jumpserver/koko/pkg/config"
	"github.com/jumpserver/koko/pkg/i18n"
	"github.com/jumpserver/koko/pkg/logger"
)

const (
	tuiPreferencesFile                 = "tui_preferences.json"
	defaultTUISidebarWidth             = 40
	minTUISidebarWidth                 = 24
	maxTUISidebarWidth                 = 72
	tuiAccentColorCount                = 9
	maxTUIUsers                        = 1000
	maxTUIConnectionPreferencesPerUser = 50
	maxTUIPreferencesFileSize          = 32 << 20
)

type tuiConnectionPreference struct {
	Account  string `json:"account"`
	Protocol string `json:"protocol"`
}

type tuiUserPreference struct {
	Organization  string                             `json:"organization,omitempty"`
	TreeMode      int                                `json:"tree_mode,omitempty"`
	Language      string                             `json:"language,omitempty"`
	LightTheme    bool                               `json:"light_theme,omitempty"`
	AccentColor   int                                `json:"accent_color,omitempty"`
	SidebarWidth  int                                `json:"sidebar_width,omitempty"`
	SidebarHidden bool                               `json:"sidebar_hidden,omitempty"`
	Connections   map[string]tuiConnectionPreference `json:"connections,omitempty"`
}

type tuiPreferenceData struct {
	Users map[string]*tuiUserPreference `json:"users"`
}

type tuiPreferences struct {
	sync.Mutex
	path  string
	users map[string]*tuiUserPreference
}

func newTUIPreferences() *tuiPreferences {
	return loadTUIPreferences(filepath.Join(config.GetConf().DataFolderPath, tuiPreferencesFile))
}

func loadTUIPreferences(path string) *tuiPreferences {
	p := &tuiPreferences{path: path, users: make(map[string]*tuiUserPreference)}
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return p
	}
	if err != nil {
		logger.Warnf("Read TUI preferences: %s", err)
		return p
	}
	if info.Size() > maxTUIPreferencesFileSize {
		logger.Warnf("Ignore oversized TUI preferences file: %s", path)
		return p
	}
	content, err := os.ReadFile(path)
	if err != nil {
		logger.Warnf("Read TUI preferences: %s", err)
		return p
	}
	var data tuiPreferenceData
	if err = json.Unmarshal(content, &data); err != nil {
		logger.Warnf("Decode TUI preferences: %s", err)
		return p
	}
	if len(data.Users) > maxTUIUsers {
		logger.Warnf("Ignore TUI preferences with too many users: %s", path)
		return p
	}
	for userID, preference := range data.Users {
		if userID == "" || preference == nil || len(preference.Connections) > maxTUIConnectionPreferencesPerUser {
			continue
		}
		if preference.TreeMode < 0 || preference.TreeMode > 2 {
			preference.TreeMode = 0
		}
		if !isTUILanguage(preference.Language) {
			preference.Language = ""
		}
		if preference.AccentColor < 0 || preference.AccentColor >= tuiAccentColorCount {
			preference.AccentColor = 0
		}
		if preference.SidebarWidth != 0 && (preference.SidebarWidth < minTUISidebarWidth || preference.SidebarWidth > maxTUISidebarWidth) {
			preference.SidebarWidth = 0
		}
		p.users[userID] = preference
	}
	return p
}

func isTUILanguage(language string) bool {
	if language == "" {
		return true
	}
	for _, code := range i18n.AllCodes {
		if language == code.String() {
			return true
		}
	}
	return false
}

func (p *tuiPreferences) userLocked(userID string) *tuiUserPreference {
	preference := p.users[userID]
	if preference != nil {
		return preference
	}
	if len(p.users) >= maxTUIUsers {
		for id := range p.users {
			delete(p.users, id)
			break
		}
	}
	preference = &tuiUserPreference{}
	p.users[userID] = preference
	return preference
}

func (p *tuiPreferences) organization(userID string) string {
	p.Lock()
	defer p.Unlock()
	if preference := p.users[userID]; preference != nil {
		return preference.Organization
	}
	return ""
}

func (p *tuiPreferences) storeOrganization(userID, organizationID string) {
	p.Lock()
	defer p.Unlock()
	preference := p.userLocked(userID)
	if preference.Organization == organizationID {
		return
	}
	preference.Organization = organizationID
	p.saveLocked()
}

func (p *tuiPreferences) treeMode(userID string) int {
	p.Lock()
	defer p.Unlock()
	if preference := p.users[userID]; preference != nil {
		return preference.TreeMode
	}
	return 0
}

func (p *tuiPreferences) storeTreeMode(userID string, mode int) {
	if mode < 0 || mode > 2 {
		return
	}
	p.Lock()
	defer p.Unlock()
	preference := p.users[userID]
	if preference == nil {
		if mode == 0 {
			return
		}
	} else if preference.TreeMode == mode {
		return
	}
	p.userLocked(userID).TreeMode = mode
	p.saveLocked()
}

func (p *tuiPreferences) display(userID, defaultLanguage string) (language string, light bool, accent, sidebarWidth int, sidebarHidden bool) {
	language, sidebarWidth = defaultLanguage, defaultTUISidebarWidth
	p.Lock()
	defer p.Unlock()
	preference := p.users[userID]
	if preference == nil {
		return
	}
	if preference.Language != "" {
		language = preference.Language
	}
	light, accent, sidebarHidden = preference.LightTheme, preference.AccentColor, preference.SidebarHidden
	if preference.SidebarWidth != 0 {
		sidebarWidth = preference.SidebarWidth
	}
	return
}

func (p *tuiPreferences) storeLanguage(userID, language string) {
	if !isTUILanguage(language) || language == "" {
		return
	}
	p.Lock()
	defer p.Unlock()
	preference := p.userLocked(userID)
	if preference.Language == language {
		return
	}
	preference.Language = language
	p.saveLocked()
}

func (p *tuiPreferences) storeAppearance(userID string, light bool, accent int) {
	if accent < 0 || accent >= tuiAccentColorCount {
		return
	}
	p.Lock()
	defer p.Unlock()
	preference := p.userLocked(userID)
	if preference.LightTheme == light && preference.AccentColor == accent {
		return
	}
	preference.LightTheme, preference.AccentColor = light, accent
	p.saveLocked()
}

func (p *tuiPreferences) storeSidebar(userID string, width int, hidden bool) {
	if width < minTUISidebarWidth || width > maxTUISidebarWidth {
		return
	}
	p.Lock()
	defer p.Unlock()
	preference := p.userLocked(userID)
	if preference.SidebarWidth == width && preference.SidebarHidden == hidden {
		return
	}
	preference.SidebarWidth, preference.SidebarHidden = width, hidden
	p.saveLocked()
}

func (p *tuiPreferences) connection(userID, assetKey string) (tuiConnectionPreference, bool) {
	p.Lock()
	defer p.Unlock()
	preference := p.users[userID]
	if preference == nil {
		return tuiConnectionPreference{}, false
	}
	connection, ok := preference.Connections[assetKey]
	return connection, ok
}

func (p *tuiPreferences) storeConnection(userID, assetKey string, connection tuiConnectionPreference) {
	p.Lock()
	defer p.Unlock()
	preference := p.userLocked(userID)
	if preference.Connections == nil {
		preference.Connections = make(map[string]tuiConnectionPreference)
	}
	if _, exists := preference.Connections[assetKey]; !exists && len(preference.Connections) >= maxTUIConnectionPreferencesPerUser {
		for key := range preference.Connections {
			delete(preference.Connections, key)
			break
		}
	}
	if existing, exists := preference.Connections[assetKey]; exists && existing == connection {
		return
	}
	preference.Connections[assetKey] = connection
	p.saveLocked()
}

func (p *tuiPreferences) saveLocked() {
	content, err := json.Marshal(tuiPreferenceData{Users: p.users})
	if err != nil {
		logger.Warnf("Encode TUI preferences: %s", err)
		return
	}
	if len(content) > maxTUIPreferencesFileSize {
		logger.Warnf("TUI preferences exceed the file size limit: %s", p.path)
		return
	}
	temporary, err := os.CreateTemp(filepath.Dir(p.path), ".tui-preferences-*")
	if err != nil {
		logger.Warnf("Create TUI preferences: %s", err)
		return
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err = temporary.Chmod(0600); err == nil {
		_, err = temporary.Write(content)
	}
	if closeErr := temporary.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(temporaryPath, p.path)
	}
	if err != nil {
		logger.Warnf("Save TUI preferences: %s", err)
	}
}
