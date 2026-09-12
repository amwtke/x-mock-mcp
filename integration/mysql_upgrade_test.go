package integration

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/go-mysql-org/go-mysql/client"
	"xmock.local/x-mock-mcp/internal/binding"
	"xmock.local/x-mock-mcp/internal/plugin/catalog"
	pluginruntime "xmock.local/x-mock-mcp/internal/plugin/runtime"
	"xmock.local/x-mock-mcp/pluginapi"
)

// Build the actual merged P0 source. Changing a new plugin's version string
// would not prove compatibility with old packages/scenarios.
func historicalMySQLRight(t *testing.T) (string, string, pluginapi.Reference) {
	t.Helper()
	// Full immutable P0 revision, retained in normal repository history.
	cmd := exec.Command("git", "archive", "--format=tar", "68ba0aaf0ca9131ae19da8a1ee932996ddd11835", "go.mod", "go.sum", "pluginapi", "contracts", "internal", "plugins/right/mysql-mock")
	cmd.Dir = ".."
	raw, err := cmd.Output()
	if err != nil {
		t.Fatalf("P0 history is required for upgrade verification: %v", err)
	}
	root := t.TempDir()
	reader := tar.NewReader(bytes.NewReader(raw))
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if header.Typeflag == tar.TypeXGlobalHeader {
			continue
		}
		name := filepath.Clean(header.Name)
		if !pluginapi.SafeRelative(filepath.ToSlash(name)) {
			t.Fatal("invalid archived path", name)
		}
		target := filepath.Join(root, name)
		if header.Typeflag == tar.TypeDir {
			if err = os.MkdirAll(target, 0700); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if header.Typeflag != tar.TypeReg {
			t.Fatal("unexpected archived file type", name)
		}
		data, err := io.ReadAll(reader)
		if err != nil {
			t.Fatal(err)
		}
		if err = os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(target, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	bin := filepath.Join(root, "mysql-mock-p0")
	cmd = exec.Command(filepath.Join(runtime.GOROOT(), "bin/go"), "build", "-buildvcs=false", "-trimpath", "-o", bin, "./plugins/right/mysql-mock")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build real P0: %v\n%s", err, out)
	}
	return packageBinary(t, bin)
}

func TestMySQLRightVersionsShareLeftAndUninstallIndependently(t *testing.T) {
	oldPath, oldSHA, oldRef := historicalMySQLRight(t)
	newPath, newSHA, newRef := packagePlugin(t, "./plugins/right/mysql-mock", "")
	if oldRef.Version != "0.1.0" || newRef.Version != "0.3.0" {
		t.Fatalf("need actual independently versioned packages, got %v and %v", oldRef, newRef)
	}
	leftPath, leftSHA, leftRef := packagePlugin(t, "./plugins/left/mysql-wire", "")
	if leftRef.Version != "0.1.0" {
		t.Fatal("left version changed unexpectedly", leftRef)
	}
	cat, err := catalog.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []struct {
		path, sha string
		ref       pluginapi.Reference
	}{{oldPath, oldSHA, oldRef}, {newPath, newSHA, newRef}, {leftPath, leftSHA, leftRef}} {
		if _, err = cat.Install(context.Background(), p.path, p.sha); err != nil {
			t.Fatal(err)
		}
		if err = cat.Enable(p.ref); err != nil {
			t.Fatal(err)
		}
	}
	factory := pluginruntime.New(cat)
	manager := binding.New(factory)
	t.Cleanup(func() {
		for _, b := range manager.List() {
			manager.Destroy(context.Background(), b.ID)
		}
	})
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	input, err := os.ReadFile(filepath.Join(root, "examples/scenarios/shop-input.json"))
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(root, "examples/scenarios/shop-candidate.json"))
	if err != nil {
		t.Fatal(err)
	}
	var active []binding.Binding
	for _, ref := range []pluginapi.Reference{oldRef, newRef} {
		report, err := factory.Prepare(context.Background(), ref, pluginapi.PreparationSpec{ProjectRoot: root, Input: input, Candidate: body})
		if err != nil || !report.Ready {
			t.Fatalf("prepare %s: %+v %v", ref.Version, report, err)
		}
		b, err := manager.Create(context.Background(), binding.Config{Left: leftRef, Right: ref, EnvironmentID: "upgrade", ResourceID: ref.Version, Contract: pluginapi.Contract{ID: "mysql.operation", Version: 1}, LeftConfig: json.RawMessage(`{"host":"127.0.0.1","port":0,"username":"mock","password":"mock-local"}`), RightConfig: json.RawMessage(`{"mode":"StrictReplay"}`), Scenario: report.CompiledBody, ScenarioVersion: 1, RequestTimeoutMS: 5000})
		if err != nil {
			t.Fatal(err)
		}
		active = append(active, b)
		queryProduct(t, b.Endpoints["mysql"])
	}
	if err = manager.Destroy(context.Background(), active[0].ID); err != nil {
		t.Fatal(err)
	}
	if err = cat.Disable(oldRef); err != nil {
		t.Fatal(err)
	}
	if err = cat.Uninstall(oldRef); err != nil {
		t.Fatal(err)
	}
	if len(cat.List()) != 2 {
		t.Fatal("old right uninstall removed a different version/role")
	}
	queryProduct(t, active[1].Endpoints["mysql"])
	if err = manager.Destroy(context.Background(), active[1].ID); err != nil {
		t.Fatal(err)
	}
	if err = cat.Disable(leftRef); err != nil {
		t.Fatal(err)
	}
	if err = cat.Uninstall(leftRef); err != nil {
		t.Fatal(err)
	}
	if len(cat.List()) != 1 || cat.List()[0].Ref != newRef {
		t.Fatal("left uninstall removed new right")
	}
	if err = cat.Disable(newRef); err != nil {
		t.Fatal(err)
	}
	if err = cat.Uninstall(newRef); err != nil {
		t.Fatal(err)
	}
	if len(cat.List()) != 0 {
		t.Fatal("plugins leaked")
	}
}

func queryProduct(t *testing.T, address string) {
	t.Helper()
	conn, err := client.Connect(address, "mock", "mock-local", "app")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	result, err := conn.Execute("SELECT id, name, price_cents, stock, status FROM products WHERE id = 1001")
	if err != nil {
		t.Fatal(err)
	}
	price, err := result.GetInt(0, 2)
	if err != nil || price != 9900 {
		t.Fatal("old/new right wire result", price, err)
	}
}
