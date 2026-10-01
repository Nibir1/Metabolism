package privacyguard

import (
	"encoding/json"
	"io"
	"os/exec"
	"strings"
	"testing"
)

// remotePrefix is the only place network code may live. Adapters there are
// opt-in by construction: nothing outside them imports them unless a user has
// configured a hosted model (Phase 2's DeepSeek and Jev adapters).
const remotePrefix = "github.com/Nibir1/metabolism/adapters/remote/"

// optInPoint is the one package allowed to import remote adapters: the CLI,
// where a person passes -allow-remote. cmd/metabolism's tests check that every
// remote provider refuses without it.
const optInPoint = "github.com/Nibir1/metabolism/cmd/metabolism"

// directlyForbidden may not be imported by any Metabolism package outside
// remotePrefix.
var directlyForbidden = map[string]bool{
	"net": true, "net/http": true, "net/rpc": true, "net/smtp": true,
	"crypto/tls": true, "net/http/httptrace": true,
}

// clientStacks may not appear anywhere in a package's dependency graph, even
// transitively. Bare "net" is allowed there, because github.com/google/uuid (via
// the SQLite driver) imports it to read interface addresses. A dependency that
// pulls in an HTTP or TLS client is a dependency that can phone home.
var clientStacks = map[string]bool{
	"net/http": true, "net/rpc": true, "net/smtp": true, "crypto/tls": true,
}

type pkg struct {
	ImportPath string
	Imports    []string
	Deps       []string
}

func listPackages(t *testing.T) []pkg {
	t.Helper()
	cmd := exec.Command("go", "list", "-json", "github.com/Nibir1/metabolism/...")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}
	var pkgs []pkg
	dec := json.NewDecoder(strings.NewReader(string(out)))
	for {
		var p pkg
		if err := dec.Decode(&p); err == io.EOF {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		pkgs = append(pkgs, p)
	}
	if len(pkgs) < 5 {
		t.Fatalf("go list found only %d packages", len(pkgs))
	}
	return pkgs
}

func TestNothingOutsideRemoteAdaptersCanReachTheNetwork(t *testing.T) {
	for _, p := range listPackages(t) {
		if strings.HasPrefix(p.ImportPath, remotePrefix) || p.ImportPath == optInPoint {
			continue
		}
		for _, imp := range p.Imports {
			if directlyForbidden[imp] {
				t.Errorf("%s imports %s; network code belongs under %s", p.ImportPath, imp, remotePrefix)
			}
			if strings.HasPrefix(imp, remotePrefix) {
				t.Errorf("%s imports the remote adapter %s; wire remote adapters only where a user opts in",
					p.ImportPath, imp)
			}
		}
		for _, dep := range p.Deps {
			if clientStacks[dep] {
				t.Errorf("%s depends on %s through a dependency", p.ImportPath, dep)
			}
		}
	}
}
