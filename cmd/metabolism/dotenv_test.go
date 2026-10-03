package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadDotEnv(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	body := "# keys\nexport DEEPSEEK_API_KEY=\"sk-test-123\"\nTYPESAFE_API_KEY=ts-456\nPATH=/evil\n\nOLLAMA_HOST='http://127.0.0.1:11434'\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DEEPSEEK_API_KEY", "")
	_ = os.Unsetenv("DEEPSEEK_API_KEY")
	t.Setenv("TYPESAFE_API_KEY", "from-the-shell") // the environment wins
	t.Setenv("OLLAMA_HOST", "")
	_ = os.Unsetenv("OLLAMA_HOST")
	path0 := os.Getenv("PATH")

	if err := loadDotEnv(path); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv("DEEPSEEK_API_KEY"); got != "sk-test-123" {
		t.Errorf("DEEPSEEK_API_KEY = %q", got)
	}
	if got := os.Getenv("TYPESAFE_API_KEY"); got != "from-the-shell" {
		t.Errorf("the file overrode the environment: %q", got)
	}
	if got := os.Getenv("OLLAMA_HOST"); got != "http://127.0.0.1:11434" {
		t.Errorf("OLLAMA_HOST = %q", got)
	}
	if os.Getenv("PATH") != path0 {
		t.Error("an unknown variable was set from the file")
	}
	_ = os.Unsetenv("DEEPSEEK_API_KEY")
}

func TestDotEnvRefusesAFileOthersCanRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	_ = os.WriteFile(path, []byte("DEEPSEEK_API_KEY=sk-secret\n"), 0o644)
	_ = os.Chmod(path, 0o644)
	err := loadDotEnv(path)
	if err == nil || !strings.Contains(err.Error(), "chmod 600") {
		t.Fatalf("a world-readable key file was read: %v", err)
	}
	if strings.Contains(err.Error(), "sk-secret") {
		t.Fatal("the error leaked the key")
	}
	if err := loadDotEnv(filepath.Join(t.TempDir(), "missing")); err != nil {
		t.Fatalf("a missing file is fine: %v", err)
	}
	bad := filepath.Join(t.TempDir(), ".env")
	_ = os.WriteFile(bad, []byte("sk-secret-without-a-name\n"), 0o600)
	if err := loadDotEnv(bad); err == nil || strings.Contains(err.Error(), "sk-secret") {
		t.Fatalf("malformed line: %v", err)
	}
}
