package vault

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitAndOpen(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	if err := Init("test-ws", "mypassword"); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if !IsInitialized("test-ws") {
		t.Fatal("expected vault to be initialized")
	}

	s, err := Open("test-ws", "mypassword")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	keys, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(keys) != 0 {
		t.Errorf("expected empty store, got %d keys", len(keys))
	}
}

func TestInitAlreadyExists(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	if err := Init("test-ws", "pass"); err != nil {
		t.Fatalf("first Init: %v", err)
	}
	if err := Init("test-ws", "pass"); err == nil {
		t.Error("expected error for duplicate Init")
	}
}

func TestOpenWrongPassword(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	if err := Init("test-ws", "correct"); err != nil {
		t.Fatalf("Init: %v", err)
	}
	_, err := Open("test-ws", "wrong")
	if !errors.Is(err, ErrWrongPassword) {
		t.Errorf("wrong password: got %v, want ErrWrongPassword", err)
	}
}

func TestOpenLocked(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	if err := Init("test-ws", "correct"); err != nil {
		t.Fatalf("Init: %v", err)
	}
	s, err := Open("test-ws", "correct")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()
	if _, err := Open("test-ws", "correct"); !errors.Is(err, ErrLocked) {
		t.Errorf("second open: got %v, want ErrLocked", err)
	}
}

func TestSetAndGet(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	if err := Init("test-ws", "pass"); err != nil {
		t.Fatalf("Init: %v", err)
	}

	s, err := Open("test-ws", "pass")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	if err := s.Set("API_KEY", "secret123"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	val, err := s.Get("API_KEY")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if val != "secret123" {
		t.Errorf("got %q, want %q", val, "secret123")
	}
}

func TestGetNotFound(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	if err := Init("test-ws", "pass"); err != nil {
		t.Fatalf("Init: %v", err)
	}

	s, err := Open("test-ws", "pass")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	_, err = s.Get("MISSING")
	if err == nil {
		t.Error("expected error for missing key")
	}
}

func TestListSorted(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	if err := Init("test-ws", "pass"); err != nil {
		t.Fatalf("Init: %v", err)
	}

	s, err := Open("test-ws", "pass")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	for _, kv := range []struct{ k, v string }{
		{"GAMMA", "g"}, {"ALPHA", "a"}, {"BETA", "b"},
	} {
		if err := s.Set(kv.k, kv.v); err != nil {
			t.Fatalf("Set %s: %v", kv.k, err)
		}
	}

	keys, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(keys) != 3 {
		t.Fatalf("expected 3 keys, got %d", len(keys))
	}
	if keys[0] != "ALPHA" || keys[1] != "BETA" || keys[2] != "GAMMA" {
		t.Errorf("keys not sorted: %v", keys)
	}
}

func TestListExcludesInternalKeys(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	if err := Init("test-ws", "pass"); err != nil {
		t.Fatalf("Init: %v", err)
	}

	s, err := Open("test-ws", "pass")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	if err := s.Set("USER_KEY", "val"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	keys, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	// Should only contain USER_KEY, not __vault_verify__
	if len(keys) != 1 {
		t.Fatalf("expected 1 key, got %d: %v", len(keys), keys)
	}
	if keys[0] != "USER_KEY" {
		t.Errorf("expected USER_KEY, got %s", keys[0])
	}
}

func TestDelete(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	if err := Init("test-ws", "pass"); err != nil {
		t.Fatalf("Init: %v", err)
	}

	s, err := Open("test-ws", "pass")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	if err := s.Set("KEY", "val"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := s.Delete("KEY"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	_, err = s.Get("KEY")
	if err == nil {
		t.Error("expected error after delete")
	}
}

func TestDeleteNotFound(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	if err := Init("test-ws", "pass"); err != nil {
		t.Fatalf("Init: %v", err)
	}

	s, err := Open("test-ws", "pass")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	if err := s.Delete("MISSING"); err == nil {
		t.Error("expected error for missing key")
	}
}

func TestAll(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	if err := Init("test-ws", "pass"); err != nil {
		t.Fatalf("Init: %v", err)
	}

	s, err := Open("test-ws", "pass")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	if err := s.Set("K1", "v1"); err != nil {
		t.Fatalf("Set K1: %v", err)
	}
	if err := s.Set("K2", "v2"); err != nil {
		t.Fatalf("Set K2: %v", err)
	}

	all, err := s.All()
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(all))
	}
	if all["K1"] != "v1" || all["K2"] != "v2" {
		t.Errorf("unexpected entries: %v", all)
	}
}

func TestImportEnvEntries(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	if err := Init("test-ws", "pass"); err != nil {
		t.Fatalf("Init: %v", err)
	}

	s, err := Open("test-ws", "pass")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	// Pre-existing key.
	if err := s.Set("EXISTING", "kept"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	entries := map[string]string{"NEW": "val", "EXISTING": "overwritten"}
	if err := s.ImportEnvEntries(entries); err != nil {
		t.Fatalf("ImportEnvEntries: %v", err)
	}

	val, err := s.Get("NEW")
	if err != nil {
		t.Fatalf("Get NEW: %v", err)
	}
	if val != "val" {
		t.Errorf("NEW = %q, want %q", val, "val")
	}

	val, err = s.Get("EXISTING")
	if err != nil {
		t.Fatalf("Get EXISTING: %v", err)
	}
	if val != "overwritten" {
		t.Errorf("EXISTING = %q, want %q", val, "overwritten")
	}
}

func TestImportEnvFile(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	if err := Init("test-ws", "pass"); err != nil {
		t.Fatalf("Init: %v", err)
	}

	envFile := filepath.Join(t.TempDir(), ".env")
	content := "API_KEY=secret123\nTOKEN=abc\n# comment\n"
	if err := os.WriteFile(envFile, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	if err := ImportEnvFile("test-ws", "pass", envFile); err != nil {
		t.Fatalf("ImportEnvFile: %v", err)
	}

	val, err := QuickGet("test-ws", "pass", "API_KEY")
	if err != nil {
		t.Fatalf("QuickGet API_KEY: %v", err)
	}
	if val != "secret123" {
		t.Errorf("API_KEY = %q, want %q", val, "secret123")
	}
}

func TestQuickSetAndGet(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	if err := Init("test-ws", "pass"); err != nil {
		t.Fatalf("Init: %v", err)
	}

	if err := QuickSet("test-ws", "pass", "KEY", "value"); err != nil {
		t.Fatalf("QuickSet: %v", err)
	}

	val, err := QuickGet("test-ws", "pass", "KEY")
	if err != nil {
		t.Fatalf("QuickGet: %v", err)
	}
	if val != "value" {
		t.Errorf("got %q, want %q", val, "value")
	}
}

func TestQuickList(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	if err := Init("test-ws", "pass"); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := QuickSet("test-ws", "pass", "K1", "v1"); err != nil {
		t.Fatalf("QuickSet: %v", err)
	}

	keys, err := QuickList("test-ws", "pass")
	if err != nil {
		t.Fatalf("QuickList: %v", err)
	}
	if len(keys) != 1 || keys[0] != "K1" {
		t.Errorf("unexpected keys: %v", keys)
	}
}

func TestIsInitialized_False(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	if IsInitialized("nonexistent-ws") {
		t.Error("expected not initialized")
	}
}

func TestDestroy(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	if err := Init("test-ws", "pass"); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if !IsInitialized("test-ws") {
		t.Fatal("expected initialized")
	}
	if err := Destroy("test-ws"); err != nil {
		t.Fatalf("Destroy: %v", err)
	}
	if IsInitialized("test-ws") {
		t.Error("expected not initialized after destroy")
	}
}

func TestExportEnvFormat(t *testing.T) {
	store := map[string]string{
		"BETA":  "b",
		"ALPHA": "a",
	}
	got := ExportEnvFormat(store)
	want := "ALPHA=a\nBETA=b\n"
	if got != want {
		t.Errorf("ExportEnvFormat = %q, want %q", got, want)
	}
}

func TestReopenPersistence(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	if err := Init("test-ws", "pass"); err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Write data.
	s, err := Open("test-ws", "pass")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := s.Set("PERSIST", "yes"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Reopen and verify data persists.
	s2, err := Open("test-ws", "pass")
	if err != nil {
		t.Fatalf("Reopen: %v", err)
	}
	defer s2.Close()

	val, err := s2.Get("PERSIST")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if val != "yes" {
		t.Errorf("got %q, want %q", val, "yes")
	}
}

// --- .env parser tests ---

func TestParseEnvFile_BasicPairs(t *testing.T) {
	data := []byte("KEY1=value1\nKEY2=value2\n")
	env := ParseEnvFile(data)
	if env["KEY1"] != "value1" {
		t.Errorf("KEY1 = %q, want %q", env["KEY1"], "value1")
	}
	if env["KEY2"] != "value2" {
		t.Errorf("KEY2 = %q, want %q", env["KEY2"], "value2")
	}
}

func TestParseEnvFile_Comments(t *testing.T) {
	data := []byte("# comment\nKEY=value\n# another comment\n")
	env := ParseEnvFile(data)
	if len(env) != 1 {
		t.Errorf("expected 1 key, got %d", len(env))
	}
}

func TestParseEnvFile_Quotes(t *testing.T) {
	tests := []struct {
		input, wantKey, wantVal string
	}{
		{"KEY='quoted value'\n", "KEY", "quoted value"},
		{`KEY="quoted value"` + "\n", "KEY", "quoted value"},
		{"KEY=unquoted\n", "KEY", "unquoted"},
	}
	for _, tt := range tests {
		env := ParseEnvFile([]byte(tt.input))
		if env[tt.wantKey] != tt.wantVal {
			t.Errorf("input %q: %s = %q, want %q", tt.input, tt.wantKey, env[tt.wantKey], tt.wantVal)
		}
	}
}

func TestParseEnvFile_EmptyAndSpaces(t *testing.T) {
	data := []byte("\n\n  KEY = value  \n\nKEY2=\n")
	env := ParseEnvFile(data)
	if env["KEY"] != "value" {
		t.Errorf("KEY = %q, want %q", env["KEY"], "value")
	}
	if env["KEY2"] != "" {
		t.Errorf("KEY2 = %q, want empty", env["KEY2"])
	}
}

func TestParseEnvFile_ValueWithEquals(t *testing.T) {
	data := []byte("KEY=value=with=equals\n")
	env := ParseEnvFile(data)
	if env["KEY"] != "value=with=equals" {
		t.Errorf("KEY = %q, want %q", env["KEY"], "value=with=equals")
	}
}

func TestUnquoteEnvValue(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{`"hello"`, "hello"},
		{`'hello'`, "hello"},
		{`hello`, "hello"},
		{`""`, ""},
		{`''`, ""},
		{`"`, `"`},
		{``, ``},
	}
	for _, tt := range tests {
		got := unquoteEnvValue(tt.input)
		if got != tt.want {
			t.Errorf("unquoteEnvValue(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestReadValueFile(t *testing.T) {
	dir := t.TempDir()
	pem := "-----BEGIN KEY-----\nabc=\n-----END KEY-----\n"
	p := filepath.Join(dir, "key.pem")
	if err := os.WriteFile(p, []byte(pem), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := ReadValueFile(p)
	if err != nil {
		t.Fatalf("ReadValueFile: %v", err)
	}
	if got != pem {
		t.Errorf("got %q, want %q (byte-for-byte)", got, pem)
	}
}

func TestReadValueFile_Errors(t *testing.T) {
	dir := t.TempDir()
	write := func(name string, data []byte) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, data, 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	tests := []struct {
		name, path, wantErr string
	}{
		{"directory", dir, "is a directory"},
		{"empty", write("empty", nil), "is empty"},
		{"binary", write("bin", []byte{0xff, 0xfe, 0x00}), "not valid UTF-8"},
		{"oversize", write("big", bytes.Repeat([]byte("a"), MaxFileValueSize+1)), "exceeds"},
	}
	// OS-specific message, so match the error kind.
	if _, err := ReadValueFile(filepath.Join(dir, "nope")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("missing: err = %v, want fs.ErrNotExist", err)
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ReadValueFile(tt.path)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("err = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestReadValueFile_AtLimit(t *testing.T) {
	p := filepath.Join(t.TempDir(), "max")
	if err := os.WriteFile(p, bytes.Repeat([]byte("a"), MaxFileValueSize), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadValueFile(p); err != nil {
		t.Errorf("value at limit rejected: %v", err)
	}
}

func TestExportEnvFormat_MultiLine(t *testing.T) {
	store := map[string]string{"PEM": "a\nb\"c\\d\r\n"}
	got := ExportEnvFormat(store)
	want := `PEM="a\nb\"c\\d\r\n"` + "\n"
	if got != want {
		t.Errorf("ExportEnvFormat = %q, want %q", got, want)
	}
}

func TestEnvFormat_RoundTrip(t *testing.T) {
	store := map[string]string{
		"PLAIN":  "value=with=equals",
		"PEM":    "-----BEGIN KEY-----\nMIIabc==\n-----END KEY-----\n",
		"TRICKY": "line1\n\"quoted\" C:\\path\\n\\\\\r\nend\\",
	}
	got := ParseEnvFile([]byte(ExportEnvFormat(store)))
	if len(got) != len(store) {
		t.Fatalf("got %d keys, want %d: %v", len(got), len(store), got)
	}
	for k, v := range store {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
}

func TestParseEnvFile_DoubleQuotedEscapes(t *testing.T) {
	env := ParseEnvFile([]byte(`A="x\ny"` + "\n" + `B="no escapes \t here"` + "\n"))
	if env["A"] != "x\ny" {
		t.Errorf("A = %q, want %q", env["A"], "x\ny")
	}
	if env["B"] != `no escapes \t here` {
		t.Errorf("B = %q, want unchanged", env["B"])
	}
}
