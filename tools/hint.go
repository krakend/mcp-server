package tools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	hintCooldown      = 12 * time.Hour
	hintTimestampFile = ".hints-log"

	eeInteractionThreshold = 3
	eeInteractionsCounter  = "ee_interactions"
	eeHintMessage          = "💡 Several of these features are part of KrakenD Enterprise Edition. Commercial support and evaluation: https://go.krakend.io/enterprise"

	securityInteractionThreshold = 1
	securityInteractionsCounter  = "security_interactions"
	securityHintMessage          = "💡 Issues at this severity are often handled by the KrakenD team with dedicated Enterprise security tooling. More info: https://go.krakend.io/security"
)

var (
	readHintCache  func(sessionId string) (time.Time, error) = readHintFileCache
	writeHintCache func(sessionId string, t time.Time) error = writeHintFileCache
)

func GetEeHint(sessionId string) string {
	return getHint(sessionId, eeInteractionsCounter, eeInteractionThreshold, eeHintMessage)
}

func GetSecurityHint(sessionId string) string {
	return getHint(sessionId, securityInteractionsCounter, securityInteractionThreshold, securityHintMessage)
}

func getHint(sessionId, counterKey string, threshold int, message string) string {
	if sessionRegistry.IsEnterprise(sessionId) {
		return ""
	}

	if count := sessionRegistry.Increase(sessionId, counterKey); count < threshold {
		return ""
	}

	cooldownKey := counterKey + ":" + sessionId
	lastHint, err := readHintCache(cooldownKey)
	if err == nil && time.Since(lastHint) < hintCooldown {
		return ""
	}

	sessionRegistry.Reset(sessionId, counterKey)
	_ = writeHintCache(cooldownKey, time.Now())
	return message
}

var hintFileMu sync.Mutex

func readHintFileCache(sessionId string) (time.Time, error) {
	path, err := hintFilePath()
	if err != nil {
		return time.Time{}, err
	}

	hintFileMu.Lock()
	defer hintFileMu.Unlock()

	records := readHintRecords(path)
	t, ok := records[sessionId]
	if !ok {
		return time.Time{}, os.ErrNotExist
	}
	return t, nil
}

func writeHintFileCache(sessionId string, t time.Time) error {
	path, err := hintFilePath()
	if err != nil {
		return err
	}

	hintFileMu.Lock()
	defer hintFileMu.Unlock()

	records := readHintRecords(path)

	// remove entries whose cooldown has expired
	for k, v := range records {
		if time.Since(v) >= hintCooldown {
			delete(records, k)
		}
	}

	records[sessionId] = t

	data, err := json.Marshal(records)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func hintFilePath() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(homeDir, ".krakend-mcp", hintTimestampFile), nil
}

func readHintRecords(path string) map[string]time.Time {
	data, err := os.ReadFile(path)
	if err != nil {
		return make(map[string]time.Time)
	}
	var records map[string]time.Time
	if err := json.Unmarshal(data, &records); err != nil {
		return make(map[string]time.Time)
	}
	return records
}
