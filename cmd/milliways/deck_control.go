package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func deckSwitchControlPath() string {
	return filepath.Join(filepath.Dir(daemonSocket()), "deck.switch")
}

func writeDeckSwitchControl(provider string) error {
	provider = strings.TrimSpace(provider)
	if provider == "" {
		return nil
	}
	path := deckSwitchControlPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	data := fmt.Sprintf("%d %s\n", time.Now().UnixNano(), provider)
	if err := os.WriteFile(tmp, []byte(data), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func newDeckSwitchControlPoller() func() (string, bool) {
	path := deckSwitchControlPath()
	var lastMod time.Time
	var lastSeq string
	if st, err := os.Stat(path); err == nil {
		lastMod = st.ModTime()
		if raw, err := os.ReadFile(path); err == nil {
			if fields := strings.Fields(string(raw)); len(fields) > 0 {
				lastSeq = fields[0]
			}
		}
	}
	return func() (string, bool) {
		st, err := os.Stat(path)
		if err != nil {
			return "", false
		}
		if !st.ModTime().After(lastMod) && !lastMod.IsZero() {
			return "", false
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return "", false
		}
		fields := strings.Fields(string(raw))
		if len(fields) < 2 {
			lastMod = st.ModTime()
			return "", false
		}
		seq, provider := fields[0], fields[1]
		lastMod = st.ModTime()
		if seq == lastSeq {
			return "", false
		}
		lastSeq = seq
		if !validChatAgent(provider) {
			return "", false
		}
		return "/switch " + provider, true
	}
}
