package main

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
)

// dotEnvFile is read at startup so keys need not be pasted into every
// terminal (or be missing from a scheduled run). It is gitignored.
const dotEnvFile = ".env"

// dotEnvKeys are the only variables read from it. Anything else in the file
// is ignored: it is a place for this tool's settings, not a way to set
// arbitrary environment.
var dotEnvKeys = map[string]bool{
	"DEEPSEEK_API_KEY":   true,
	"TYPESAFE_API_KEY":   true,
	"OLLAMA_HOST":        true,
	"METABOLISM_JOURNAL": true,
}

// loadDotEnv sets the known variables from path, without overriding any
// already in the environment. A missing file is fine. A file others can
// read is refused, since it holds keys. Values are never printed.
func loadDotEnv(path string) error {
	info, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("%s is readable by others (mode %o); it holds keys, so it is not read until: chmod 600 %s",
			path, info.Mode().Perm(), path)
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return fmt.Errorf("%s:%d: not KEY=value", path, n) // the line itself is not echoed: it may be a key
		}
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		if len(value) >= 2 && (value[0] == '"' || value[0] == '\'') && value[len(value)-1] == value[0] {
			value = value[1 : len(value)-1]
		}
		if !dotEnvKeys[key] || value == "" {
			continue
		}
		if _, set := os.LookupEnv(key); set {
			continue
		}
		if err := os.Setenv(key, value); err != nil {
			return err
		}
	}
	return sc.Err()
}
