package handler

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jumpserver-dev/sdk-go/model"

	"github.com/jumpserver/koko/pkg/config"
	"github.com/jumpserver/koko/pkg/i18n"
	"github.com/jumpserver/koko/pkg/logger"
)

const (
	assetTUIMinWidth                    = 80
	assetTUIMinHeight                   = 24
	terminalPreferenceVersion           = 1
	terminalPreferenceRecentLimit       = 5
	terminalPreferenceAssetLimit        = 2048
	terminalPreferenceFileMaxSize int64 = 4 << 20
	terminalPreferenceDirName           = "user_preferences"
)

type terminalInterfaceMode string

const (
	terminalInterfaceModeTUI  terminalInterfaceMode = "tui"
	terminalInterfaceModeText terminalInterfaceMode = "text"
)

type terminalMouseMode string

const (
	terminalMouseModeKoko   terminalMouseMode = "koko"
	terminalMouseModeClient terminalMouseMode = "client"
)

type terminalConnectionPreference struct {
	AccountAlias    string `json:"account_alias,omitempty"`
	AccountName     string `json:"account_name,omitempty"`
	AccountUsername string `json:"account_username,omitempty"`
	Protocol        string `json:"protocol"`
}

type terminalAssetPreference struct {
	UpdatedAt int64                          `json:"updated_at"`
	Recent    []terminalConnectionPreference `json:"recent,omitempty"`
}

type terminalUserPreference struct {
	Version       int                                `json:"version"`
	InterfaceMode terminalInterfaceMode              `json:"interface_mode,omitempty"`
	Language      string                             `json:"language,omitempty"`
	Assets        map[string]terminalAssetPreference `json:"assets,omitempty"`
}

type terminalPreferenceStore struct {
	mu  sync.Mutex
	dir string
}

var defaultTerminalPreferenceStore = newTerminalPreferenceStore("")

func newTerminalPreferenceStore(dir string) *terminalPreferenceStore {
	return &terminalPreferenceStore{dir: dir}
}

func (s *terminalPreferenceStore) directory() string {
	if s.dir != "" {
		return s.dir
	}
	return filepath.Join(config.GetConf().DataFolderPath, terminalPreferenceDirName)
}

func (s *terminalPreferenceStore) userFile(userID string) string {
	sum := sha256.Sum256([]byte(userID))
	return filepath.Join(s.directory(), hex.EncodeToString(sum[:])+".json")
}

func (s *terminalPreferenceStore) load(userID string) (terminalUserPreference, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadLocked(userID)
}

func (s *terminalPreferenceStore) loadLocked(userID string) (terminalUserPreference, error) {
	preference := defaultTerminalUserPreference()
	file, err := os.Open(s.userFile(userID))
	if errors.Is(err, os.ErrNotExist) {
		return preference, nil
	}
	if err != nil {
		return preference, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, terminalPreferenceFileMaxSize+1))
	if err != nil {
		return preference, err
	}
	if int64(len(data)) > terminalPreferenceFileMaxSize {
		return preference, fmt.Errorf("terminal preference file exceeds %d bytes", terminalPreferenceFileMaxSize)
	}
	if err = json.Unmarshal(data, &preference); err != nil {
		return defaultTerminalUserPreference(), err
	}
	if preference.Version != terminalPreferenceVersion {
		return defaultTerminalUserPreference(), fmt.Errorf(
			"unsupported terminal preference version %d", preference.Version,
		)
	}
	normalizeTerminalUserPreference(&preference)
	return preference, nil
}

func (s *terminalPreferenceStore) update(userID string,
	update func(*terminalUserPreference)) (terminalUserPreference, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	preference, err := s.loadLocked(userID)
	if err != nil {
		preference = defaultTerminalUserPreference()
	}
	update(&preference)
	normalizeTerminalUserPreference(&preference)
	if err = s.writeLocked(userID, preference); err != nil {
		return preference, err
	}
	return preference, nil
}

func (s *terminalPreferenceStore) writeLocked(userID string, preference terminalUserPreference) error {
	directory := s.directory()
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(directory, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(preference, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if int64(len(data)) > terminalPreferenceFileMaxSize {
		return fmt.Errorf("terminal preference data exceeds %d bytes", terminalPreferenceFileMaxSize)
	}
	tempFile, err := os.CreateTemp(directory, ".terminal-preference-*")
	if err != nil {
		return err
	}
	tempPath := tempFile.Name()
	defer os.Remove(tempPath)
	if err = tempFile.Chmod(0o600); err == nil {
		_, err = tempFile.Write(data)
	}
	if err == nil {
		err = tempFile.Sync()
	}
	closeErr := tempFile.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(tempPath, s.userFile(userID))
}

func defaultTerminalUserPreference() terminalUserPreference {
	return terminalUserPreference{
		Version: terminalPreferenceVersion, InterfaceMode: terminalInterfaceModeTUI,
		Assets: make(map[string]terminalAssetPreference),
	}
}

func normalizeTerminalUserPreference(preference *terminalUserPreference) {
	preference.Version = terminalPreferenceVersion
	preference.InterfaceMode = normalizeTerminalInterfaceMode(preference.InterfaceMode)
	if _, ok := supportedTerminalLanguage(preference.Language); !ok {
		preference.Language = ""
	}
	if preference.Assets == nil {
		preference.Assets = make(map[string]terminalAssetPreference)
	}
	for key, assetPreference := range preference.Assets {
		assetPreference.Recent = normalizeRecentConnections(assetPreference.Recent)
		if len(assetPreference.Recent) == 0 {
			delete(preference.Assets, key)
			continue
		}
		preference.Assets[key] = assetPreference
	}
	if len(preference.Assets) <= terminalPreferenceAssetLimit {
		return
	}
	keys := make([]string, 0, len(preference.Assets))
	for key := range preference.Assets {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		return preference.Assets[keys[i]].UpdatedAt > preference.Assets[keys[j]].UpdatedAt
	})
	for _, key := range keys[terminalPreferenceAssetLimit:] {
		delete(preference.Assets, key)
	}
}

func normalizeRecentConnections(recent []terminalConnectionPreference) []terminalConnectionPreference {
	result := make([]terminalConnectionPreference, 0, min(len(recent), terminalPreferenceRecentLimit))
	for _, item := range recent {
		item.Protocol = strings.TrimSpace(item.Protocol)
		if item.Protocol == "" || (item.AccountAlias == "" && item.AccountName == "" && item.AccountUsername == "") {
			continue
		}
		duplicate := false
		for _, existing := range result {
			if sameTerminalConnectionPreference(existing, item) {
				duplicate = true
				break
			}
		}
		if !duplicate {
			result = append(result, item)
		}
		if len(result) == terminalPreferenceRecentLimit {
			break
		}
	}
	return result
}

func sameTerminalConnectionPreference(left, right terminalConnectionPreference) bool {
	return samePermAccount(left.account(), right.account()) && strings.EqualFold(left.Protocol, right.Protocol)
}

func (p terminalConnectionPreference) account() model.PermAccount {
	return model.PermAccount{Alias: p.AccountAlias, Name: p.AccountName, Username: p.AccountUsername}
}

func newTerminalConnectionPreference(account model.PermAccount, protocol string) terminalConnectionPreference {
	return terminalConnectionPreference{
		AccountAlias: account.Alias, AccountName: account.Name,
		AccountUsername: account.Username, Protocol: strings.TrimSpace(protocol),
	}
}

func supportedTerminalLanguage(value string) (string, bool) {
	value = strings.TrimSpace(value)
	for _, code := range i18n.AllCodes {
		if strings.EqualFold(value, code.String()) {
			return code.String(), true
		}
	}
	return "", false
}

func normalizeTerminalInterfaceMode(value terminalInterfaceMode) terminalInterfaceMode {
	if value == terminalInterfaceModeText {
		return value
	}
	return terminalInterfaceModeTUI
}

func terminalWindowSupportsTUI(width, height int) bool {
	width, height = assetTUITerminalSize(width, height)
	return width >= assetTUIMinWidth && height >= assetTUIMinHeight
}

func terminalShouldUseTextMode(width, height int, interfaceMode terminalInterfaceMode) bool {
	return !terminalWindowSupportsTUI(width, height) ||
		normalizeTerminalInterfaceMode(interfaceMode) == terminalInterfaceModeText
}

func terminalAssetPreferenceKey(asset model.PermAsset) string {
	if asset.ID == "" {
		return ""
	}
	return asset.OrgID + ":" + asset.ID
}

func (h *InteractiveHandler) terminalPreferenceStorage() *terminalPreferenceStore {
	if h.preferenceStore != nil {
		return h.preferenceStore
	}
	return defaultTerminalPreferenceStore
}

func (h *InteractiveHandler) loadTerminalPreference() {
	h.interfaceMode = terminalInterfaceModeTUI
	h.mouseMode = terminalMouseModeKoko
	if h.user == nil || h.user.ID == "" {
		return
	}
	preference, err := h.terminalPreferenceStorage().load(h.user.ID)
	if err != nil {
		logger.Warnf("Load user terminal preference failed: %s", err)
		return
	}
	h.interfaceMode = preference.InterfaceMode
	if language, ok := supportedTerminalLanguage(preference.Language); ok {
		h.i18nLang = language
		if h.jmsService != nil {
			setAPIClientLang(h.jmsService, language)
		}
	}
}

func (h *InteractiveHandler) saveTerminalPreference(interfaceMode terminalInterfaceMode) {
	h.interfaceMode = normalizeTerminalInterfaceMode(interfaceMode)
	if h.user == nil || h.user.ID == "" {
		return
	}
	_, err := h.terminalPreferenceStorage().update(h.user.ID, func(preference *terminalUserPreference) {
		preference.InterfaceMode = h.interfaceMode
	})
	if err != nil {
		logger.Warnf("Save user terminal interface mode failed: %s", err)
		return
	}
}

func (h *InteractiveHandler) saveTerminalLanguage(language string) {
	language, ok := supportedTerminalLanguage(language)
	if !ok || h.user == nil || h.user.ID == "" {
		return
	}
	_, err := h.terminalPreferenceStorage().update(h.user.ID, func(preference *terminalUserPreference) {
		preference.Language = language
	})
	if err != nil {
		logger.Warnf("Save user terminal language failed: %s", err)
		return
	}
}

func (h *InteractiveHandler) loadRecentConnectionPreferences(asset model.PermAsset) []terminalConnectionPreference {
	if h.user == nil || h.user.ID == "" {
		return nil
	}
	key := terminalAssetPreferenceKey(asset)
	if key == "" {
		return nil
	}
	preference, err := h.terminalPreferenceStorage().load(h.user.ID)
	if err != nil {
		logger.Warnf("Load user terminal connection preference failed: %s", err)
		return nil
	}
	recent := preference.Assets[key].Recent
	return append([]terminalConnectionPreference(nil), recent...)
}

func (h *InteractiveHandler) saveLastConnectionPreference(asset model.PermAsset,
	account model.PermAccount, protocol string) {
	if h.user == nil || h.user.ID == "" {
		return
	}
	key := terminalAssetPreferenceKey(asset)
	connection := newTerminalConnectionPreference(account, protocol)
	if key == "" || connection.Protocol == "" {
		return
	}
	_, err := h.terminalPreferenceStorage().update(h.user.ID, func(preference *terminalUserPreference) {
		assetPreference := preference.Assets[key]
		assetPreference.UpdatedAt = time.Now().UnixNano()
		assetPreference.Recent = normalizeRecentConnections(append(
			[]terminalConnectionPreference{connection}, assetPreference.Recent...,
		))
		preference.Assets[key] = assetPreference
	})
	if err != nil {
		logger.Warnf("Save user terminal connection preference failed: %s", err)
		return
	}
}
