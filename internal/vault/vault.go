// ExitBox - Multi-Agent Container Sandbox
// Copyright (C) 2026 Cloud Exit B.V.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

// Package vault provides encrypted key-value secret storage using Badger
// (embedded KV store) with AES encryption and Argon2id password-based key
// derivation. Each workspace gets its own encrypted database.
package vault

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	badger "github.com/dgraph-io/badger/v4"
	"golang.org/x/crypto/argon2"
)

const (
	// Argon2id parameters (OWASP recommended).
	argonTime    = 3
	argonMemory  = 64 * 1024 // 64 MB
	argonThreads = 4
	argonKeyLen  = 32 // AES-256

	saltLen = 32

	// verifyKey is a sentinel key used to verify the password is correct.
	verifyKey   = "__vault_verify__"
	verifyValue = "exitbox-vault-v1"
)

// Store wraps a Badger database for encrypted secret storage.
type Store struct {
	db *badger.DB
}

// Init creates a new vault with an empty encrypted database.
func Init(workspace, password string) error {
	dir := vaultDir(workspace)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("creating vault directory: %w", err)
	}

	saltPath := filepath.Join(dir, "salt")
	if _, err := os.Stat(saltPath); err == nil {
		return fmt.Errorf("vault already exists for workspace %q", workspace)
	}

	// Generate and store salt.
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return fmt.Errorf("generating salt: %w", err)
	}
	if err := os.WriteFile(saltPath, salt, 0600); err != nil {
		return fmt.Errorf("writing salt: %w", err)
	}

	key := deriveKey(password, salt)

	db, err := openDB(filepath.Join(dir, "db"), key)
	if err != nil {
		// Clean up on failure.
		os.Remove(saltPath)
		return fmt.Errorf("creating database: %w", err)
	}

	// Write verification entry.
	err = db.Update(func(txn *badger.Txn) error {
		return txn.Set([]byte(verifyKey), []byte(verifyValue))
	})
	if err != nil {
		db.Close()
		os.RemoveAll(filepath.Join(dir, "db"))
		os.Remove(saltPath)
		return fmt.Errorf("writing verification entry: %w", err)
	}

	return db.Close()
}

// IsInitialized checks whether a vault exists for the workspace.
func IsInitialized(workspace string) bool {
	saltPath := filepath.Join(vaultDir(workspace), "salt")
	_, err := os.Stat(saltPath)
	return err == nil
}

// Open decrypts and opens the vault database, returning a Store handle.
// The caller must call Close() when done.
func Open(workspace, password string) (*Store, error) {
	dir := vaultDir(workspace)

	salt, err := os.ReadFile(filepath.Join(dir, "salt"))
	if err != nil {
		return nil, fmt.Errorf("reading salt (vault not initialized?): %w", err)
	}
	if len(salt) != saltLen {
		return nil, fmt.Errorf("corrupted salt file")
	}

	key := deriveKey(password, salt)

	db, err := openDB(filepath.Join(dir, "db"), key)
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	// Verify password by reading the verification entry.
	var verified bool
	err = db.View(func(txn *badger.Txn) error {
		item, getErr := txn.Get([]byte(verifyKey))
		if getErr != nil {
			return getErr
		}
		return item.Value(func(val []byte) error {
			verified = string(val) == verifyValue
			return nil
		})
	})
	if err != nil || !verified {
		db.Close()
		return nil, fmt.Errorf("wrong password or corrupted vault")
	}

	return &Store{db: db}, nil
}

// Close closes the vault database.
func (s *Store) Close() error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

// Get reads a single secret by key.
func (s *Store) Get(key string) (string, error) {
	var val string
	err := s.db.View(func(txn *badger.Txn) error {
		item, getErr := txn.Get([]byte(key))
		if getErr == badger.ErrKeyNotFound {
			return fmt.Errorf("key %q not found in vault", key)
		}
		if getErr != nil {
			return getErr
		}
		return item.Value(func(v []byte) error {
			val = string(v)
			return nil
		})
	})
	return val, err
}

// Set writes a key-value pair to the vault.
func (s *Store) Set(key, value string) error {
	return s.db.Update(func(txn *badger.Txn) error {
		return txn.Set([]byte(key), []byte(value))
	})
}

// Delete removes a key from the vault.
func (s *Store) Delete(key string) error {
	return s.db.Update(func(txn *badger.Txn) error {
		// Check existence first.
		_, err := txn.Get([]byte(key))
		if err == badger.ErrKeyNotFound {
			return fmt.Errorf("key %q not found in vault", key)
		}
		if err != nil {
			return err
		}
		return txn.Delete([]byte(key))
	})
}

// List returns sorted key names from the vault (excluding internal keys).
func (s *Store) List() ([]string, error) {
	var keys []string
	err := s.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = false
		it := txn.NewIterator(opts)
		defer it.Close()
		for it.Rewind(); it.Valid(); it.Next() {
			k := string(it.Item().Key())
			if strings.HasPrefix(k, "__") && strings.HasSuffix(k, "__") {
				continue // skip internal keys
			}
			keys = append(keys, k)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(keys)
	return keys, nil
}

// All returns all key-value pairs from the vault (excluding internal keys).
func (s *Store) All() (map[string]string, error) {
	store := make(map[string]string)
	err := s.db.View(func(txn *badger.Txn) error {
		it := txn.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()
		for it.Rewind(); it.Valid(); it.Next() {
			item := it.Item()
			k := string(item.Key())
			if strings.HasPrefix(k, "__") && strings.HasSuffix(k, "__") {
				continue
			}
			if err := item.Value(func(v []byte) error {
				store[k] = string(v)
				return nil
			}); err != nil {
				return err
			}
		}
		return nil
	})
	return store, err
}

// ImportEnvEntries merges key-value pairs from a parsed .env map into the vault.
func (s *Store) ImportEnvEntries(entries map[string]string) error {
	return s.db.Update(func(txn *badger.Txn) error {
		for k, v := range entries {
			if err := txn.Set([]byte(k), []byte(v)); err != nil {
				return err
			}
		}
		return nil
	})
}

// ReplaceAll opens the vault and replaces all entries with the given store.
// Existing entries not in the new store are deleted.
func ReplaceAll(workspace, password string, store map[string]string) error {
	s, err := Open(workspace, password)
	if err != nil {
		return err
	}
	defer s.Close()

	// Delete all existing user entries.
	existing, err := s.All()
	if err != nil {
		return err
	}
	for k := range existing {
		if delErr := s.Delete(k); delErr != nil {
			return delErr
		}
	}

	// Write new entries.
	return s.ImportEnvEntries(store)
}

// --- Convenience one-shot functions (open, do, close) ---

// QuickGet opens the vault, reads a key, and closes.
func QuickGet(workspace, password, key string) (string, error) {
	s, err := Open(workspace, password)
	if err != nil {
		return "", err
	}
	defer s.Close()
	return s.Get(key)
}

// QuickSet opens the vault, writes a key-value pair, and closes.
func QuickSet(workspace, password, key, value string) error {
	s, err := Open(workspace, password)
	if err != nil {
		return err
	}
	defer s.Close()
	return s.Set(key, value)
}

// QuickDelete opens the vault, deletes a key, and closes.
func QuickDelete(workspace, password, key string) error {
	s, err := Open(workspace, password)
	if err != nil {
		return err
	}
	defer s.Close()
	return s.Delete(key)
}

// QuickList opens the vault, lists keys, and closes.
func QuickList(workspace, password string) ([]string, error) {
	s, err := Open(workspace, password)
	if err != nil {
		return nil, err
	}
	defer s.Close()
	return s.List()
}

// ImportEnvFile reads a .env file and merges its key-value pairs into the vault.
func ImportEnvFile(workspace, password, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading source file: %w", err)
	}
	parsed := ParseEnvFile(data)
	if len(parsed) == 0 {
		return fmt.Errorf("no key-value pairs found in %s", path)
	}

	s, openErr := Open(workspace, password)
	if openErr != nil {
		return openErr
	}
	defer s.Close()
	return s.ImportEnvEntries(parsed)
}

// Destroy removes the entire vault directory for a workspace.
func Destroy(workspace string) error {
	return os.RemoveAll(vaultDir(workspace))
}

// ExportEnvFormat returns key-value pairs as KEY=VALUE lines.
func ExportEnvFormat(store map[string]string) string {
	keys := make([]string, 0, len(store))
	for k := range store {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var buf strings.Builder
	for _, k := range keys {
		fmt.Fprintf(&buf, "%s=%s\n", k, store[k])
	}
	return buf.String()
}

// --- internal helpers ---

// deriveKey derives a 256-bit encryption key from password and salt using Argon2id.
func deriveKey(password string, salt []byte) []byte {
	return argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
}

// openDB opens a Badger database with the given encryption key.
func openDB(dir string, encryptionKey []byte) (*badger.DB, error) {
	opts := badger.DefaultOptions(dir).
		WithEncryptionKey(encryptionKey).
		WithIndexCacheSize(1 << 20). // 1 MB
		WithLogger(nil)              // suppress Badger logs
	return badger.Open(opts)
}

// vaultDir returns the vault directory for a workspace.
func vaultDir(workspace string) string {
	return filepath.Join(dataDir(), "vaults", workspace)
}

// dataDir returns the XDG data directory for exitbox.
func dataDir() string {
	if v := os.Getenv("XDG_DATA_HOME"); v != "" {
		return filepath.Join(v, "exitbox")
	}
	home := os.Getenv("HOME")
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	return filepath.Join(home, ".local", "share", "exitbox")
}

// --- .env parser ---

// ParseEnvFile parses KEY=VALUE lines from .env file data.
func ParseEnvFile(data []byte) map[string]string {
	env := make(map[string]string)
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.IndexByte(line, '=')
		if idx < 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		if key == "" {
			continue
		}
		val := strings.TrimSpace(line[idx+1:])
		val = unquoteEnvValue(val)
		env[key] = val
	}
	return env
}

// unquoteEnvValue strips matching single or double quotes from a value.
func unquoteEnvValue(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}
