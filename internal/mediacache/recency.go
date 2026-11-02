package mediacache

import (
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Recency stores only changed small records, never immutable chunk digests.
// Bound both decoding and writing independently of the larger audio manifest.
const maxRecency = 16 * 1024 * 1024
const maxRecencyRecords = maxRecency / 256

func validGeneration(g string) bool {
	_, err := hex.DecodeString(g)
	return len(g) == 32 && err == nil && strings.ToLower(g) == g
}

type recencyRecord struct {
	Generation string
	BaseAccess int64
	LastAccess int64
}

func (m *Manager) saveRecency() error {
	// Failed writes obey the same five-second retry bound as successful ones.
	m.lastSaveAttempt = m.opt.Now()
	records := make(map[string]recencyRecord)
	for key, e := range m.entries {
		if !m.invalid[key] && !m.pending[key] && e.Generation != "" && e.LastAccess != e.durableAccess {
			if len(records) == maxRecencyRecords || !validGeneration(e.Generation) {
				return errCache
			}
			records[key] = recencyRecord{e.Generation, e.durableAccess, e.LastAccess}
		}
	}
	b, err := json.Marshal(records)
	if err != nil || len(b) > maxRecency {
		return errCache
	}
	if err := m.writeSnapshot("recency", b); err != nil {
		return err
	}
	m.recencyDirty = false
	return nil
}

func (m *Manager) loadRecency() {
	f, err := os.Open(filepath.Join(m.opt.Root, "recency.json"))
	if err != nil {
		return
	}
	b, err := io.ReadAll(io.LimitReader(f, maxRecency+1))
	_ = f.Close()
	if err != nil || len(b) > maxRecency {
		return
	}
	var records map[string]recencyRecord
	if json.Unmarshal(b, &records) != nil || len(records) > maxRecencyRecords {
		return
	}
	for key, r := range records {
		e := m.entries[key]
		if e != nil && e.Generation != "" && r.Generation == e.Generation && r.BaseAccess == e.durableAccess && r.LastAccess >= e.LastAccess {
			e.LastAccess = r.LastAccess
		}
	}
}

// Caller holds mu. Sync the temporary file before atomically replacing the
// previous snapshot, so a failed write preserves the last complete document.
func (m *Manager) writeSnapshot(name string, b []byte) error {
	p := filepath.Join(m.opt.Root, name+".tmp")
	f, err := os.OpenFile(p, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return errCache
	}
	_, err = f.Write(b)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil || closeErr != nil {
		return errCache
	}
	if os.Rename(p, filepath.Join(m.opt.Root, name+".json")) != nil {
		return errCache
	}
	return nil
}
