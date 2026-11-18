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
	readHintCache  func(key string) (time.Time, error) = readHintFileCache
	writeHintCache func(key string, t time.Time) error = writeHintFileCache
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

	// The cooldown is shared by every session of this OS user (the file lives
	// in their home), so restarting the client does not show the hint again.
	lastHint, err := readHintCache(counterKey)
	if err == nil && time.Since(lastHint) < hintCooldown {
		return ""
	}

	sessionRegistry.Reset(sessionId, counterKey)
	// A failed write only means the cooldown is not persisted.
	_ = writeHintCache(counterKey, time.Now())
	return message
}

var hintFileMu sync.Mutex

func readHintFileCache(key string) (time.Time, error) {
	path, err := hintFilePath()
	if err != nil {
		return time.Time{}, err
	}

	hintFileMu.Lock()
	defer hintFileMu.Unlock()

	records := readHintRecords(path)
	t, ok := records[key]
	if !ok {
		return time.Time{}, os.ErrNotExist
	}
	return t, nil
}

func writeHintFileCache(key string, t time.Time) error {
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

	records[key] = t

	data, err := json.Marshal(records)
	if err != nil {
		return err
	}
	return writeFileAtomic(path, data)
}

// writeFileAtomic replaces path through a temp file and a rename, so another
// server process reading the file never sees it half written.
func writeFileAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // no-op once the rename succeeded

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
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
