package integration

import (
	"archive/zip"
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
	"xmock.local/x-mock-mcp/internal/binding"
	"xmock.local/x-mock-mcp/internal/plugin/catalog"
	pluginruntime "xmock.local/x-mock-mcp/internal/plugin/runtime"
	"xmock.local/x-mock-mcp/pluginapi"
)

func packagePlugin(t *testing.T, source, alias string) (string, string, pluginapi.Reference) {
	t.Helper()
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(t.TempDir(), "plugin")
	args := []string{"build", "-o", bin}
	if alias != "" {
		args = append(args, "-ldflags=-X main.pluginID="+alias)
	}
	args = append(args, source)
	cmd := exec.Command(filepath.Join(runtime.GOROOT(), "bin", "go"), args...)
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build plugin: %v\n%s", err, out)
	}
	raw, err := exec.Command(bin, "--describe").Output()
	if err != nil {
		t.Fatal(err)
	}
	var desc pluginapi.Descriptor
	if err = pluginapi.Decode(raw, &desc); err != nil {
		t.Fatal(err)
	}
	binary, err := os.ReadFile(bin)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(binary)
	m := pluginapi.Manifest{Descriptor: desc, Platform: runtime.GOOS + "/" + runtime.GOARCH, Entrypoint: "bin/plugin", Files: map[string]string{"bin/plugin": hex.EncodeToString(sum[:])}}
	manifest, _ := json.Marshal(m)
	p := filepath.Join(t.TempDir(), "plugin.zip")
	f, _ := os.Create(p)
	z := zip.NewWriter(f)
	for _, file := range []struct {
		name string
		data []byte
		mode os.FileMode
	}{{"manifest.json", manifest, 0600}, {"bin/plugin", binary, 0700}} {
		h := &zip.FileHeader{Name: file.name, Method: zip.Deflate}
		h.SetMode(file.mode)
		w, err := z.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = w.Write(file.data); err != nil {
			t.Fatal(err)
		}
	}
	if err = z.Close(); err != nil {
		t.Fatal(err)
	}
	f.Close()
	b, _ := os.ReadFile(p)
	s := sha256.Sum256(b)
	return p, hex.EncodeToString(s[:]), desc.Ref
}
func echoEnvironment(t *testing.T) (*catalog.Catalog, *binding.Manager, binding.Config) {
	t.Helper()
	c, err := catalog.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	refs := map[pluginapi.Role]pluginapi.Reference{}
	for _, source := range []string{"./internal/testkit/plugins/left-echo", "./internal/testkit/plugins/right-echo"} {
		p, d, ref := packagePlugin(t, source, "")
		if _, err = c.Install(context.Background(), p, d); err != nil {
			t.Fatal(err)
		}
		if err = c.Enable(ref); err != nil {
			t.Fatal(err)
		}
		refs[ref.Role] = ref
	}
	m := binding.New(pluginruntime.New(c))
	t.Cleanup(func() {
		for _, b := range m.List() {
			m.Destroy(context.Background(), b.ID)
		}
	})
	return c, m, binding.Config{Left: refs[pluginapi.Left], Right: refs[pluginapi.Right], Contract: pluginapi.Contract{ID: "echo", Version: 1}, EnvironmentID: "env", ResourceID: "echo", LeftConfig: json.RawMessage(`{"listen":"127.0.0.1:0"}`), RightConfig: json.RawMessage(`{"prefix":"A:"}`), Scenario: json.RawMessage(`{}`)}
}
func echo(t *testing.T, address, value string) string {
	t.Helper()
	conn, err := net.DialTimeout("tcp", address, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(2 * time.Second))
	raw, _ := json.Marshal(map[string]string{"value": value})
	conn.Write(append(raw, '\n'))
	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil {
		t.Fatal(err)
	}
	var response map[string]string
	if err = json.Unmarshal(line, &response); err != nil {
		t.Fatal(err)
	}
	return response["value"]
}
func TestPluginLifecycle(t *testing.T) {
	c, m, conf := echoEnvironment(t)
	ctx := context.Background()
	b, err := m.Create(ctx, conf)
	if err != nil {
		t.Fatal(err)
	}
	if value := echo(t, b.Endpoints["tcp"], "hello"); value != "A:hello" {
		t.Fatal(value)
	}
	if pluginapi.Code(c.Disable(conf.Left)) != "PLUGIN_IN_USE" {
		t.Fatal("disabled active left")
	}
	if err = m.Destroy(ctx, b.ID); err != nil {
		t.Fatal(err)
	}
	for _, r := range c.List() {
		if r.ActiveInstances != 0 {
			t.Fatal("process reference leaked", r)
		}
	}
	if err = c.Disable(conf.Left); err != nil {
		t.Fatal(err)
	}
	if err = c.Uninstall(conf.Left); err != nil {
		t.Fatal(err)
	}
	if len(c.List()) != 1 || c.List()[0].Ref.Role != pluginapi.Right {
		t.Fatal("right was uninstalled with left")
	}
	c.Disable(conf.Right)
	if err = c.Uninstall(conf.Right); err != nil {
		t.Fatal(err)
	}
	if conn, err := net.DialTimeout("tcp", b.Endpoints["tcp"], 100*time.Millisecond); err == nil {
		conn.Close()
		t.Fatal("listener survived destroy")
	}
}
func TestPluginCrashIsolation(t *testing.T) {
	c, m, conf := echoEnvironment(t)
	ctx := context.Background()
	a, err := m.Create(ctx, conf)
	if err != nil {
		t.Fatal(err)
	}
	conf.EnvironmentID = "env-b"
	conf.RightConfig = json.RawMessage(`{"prefix":"B:"}`)
	b, err := m.Create(ctx, conf)
	if err != nil {
		t.Fatal(err)
	}
	right, _ := m.Right(a.ID)
	pid := right.(interface{ PID() int }).PID()
	if err = syscall.Kill(pid, syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for m.Failure(a.ID) == nil && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if m.Failure(a.ID) == nil {
		t.Fatal("crashed plugin not reported")
	}
	if value := echo(t, b.Endpoints["tcp"], "live"); value != "B:live" {
		t.Fatal(value)
	}
	m.Destroy(ctx, b.ID)
	for time.Now().Before(deadline) {
		active := 0
		for _, r := range c.List() {
			active += r.ActiveInstances
		}
		if active == 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("crashed binding did not release references")
}
func TestPluginExtensionWithoutCoreImport(t *testing.T) {
	c, m, conf := echoEnvironment(t)
	p, d, ref := packagePlugin(t, "./internal/testkit/plugins/right-echo", "different-right")
	if _, err := c.Install(context.Background(), p, d); err != nil {
		t.Fatal(err)
	}
	c.Enable(ref)
	conf.Right = ref
	b, err := m.Create(context.Background(), conf)
	if err != nil {
		t.Fatal(err)
	}
	if echo(t, b.Endpoints["tcp"], "extension") != "A:extension" {
		t.Fatal("installed extension did not route")
	}
	cmd := exec.Command(filepath.Join(runtime.GOROOT(), "bin", "go"), "list", "-deps", "./internal/binding", "./internal/plugin/catalog")
	cmd.Dir = ".."
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"contracts/mysqlv1", "/plugins/left/", "/plugins/right/", "modelcontextprotocol"} {
		if strings.Contains(string(out), forbidden) {
			t.Fatal("concrete dependency leaked into core", forbidden)
		}
	}
}

func TestPluginPortConflictRollsBackAndPreservesOwner(t *testing.T) {
	c, m, conf := echoEnvironment(t)
	owner, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	conf.LeftConfig, _ = json.Marshal(map[string]string{"listen": owner.Addr().String()})
	if _, err = m.Create(context.Background(), conf); pluginapi.Code(err) != "PORT_IN_USE" {
		t.Fatal("expected port conflict", err)
	}
	if len(m.List()) != 0 {
		t.Fatal("failed binding published")
	}
	for _, r := range c.List() {
		if r.ActiveInstances != 0 {
			t.Fatal("startup rollback leaked process reference")
		}
	}
	conn, err := net.DialTimeout("tcp", owner.Addr().String(), time.Second)
	if err != nil {
		t.Fatal("pre-existing listener disturbed", err)
	}
	conn.Close()
}

func TestPluginPreparationWithoutLeftOrEnvironment(t *testing.T) {
	c, _ := catalog.Open(t.TempDir())
	p, d, ref := packagePlugin(t, "./internal/testkit/plugins/right-echo", "")
	if _, err := c.Install(context.Background(), p, d); err != nil {
		t.Fatal(err)
	}
	factory := pluginruntime.New(c)
	spec := pluginapi.PreparationSpec{ProjectRoot: t.TempDir(), Input: json.RawMessage(`{"evidence":"opaque"}`), Candidate: json.RawMessage(`{"value":"prepared"}`)}
	if _, err := factory.Prepare(context.Background(), ref, spec); pluginapi.Code(err) != "PLUGIN_NOT_ENABLED" {
		t.Fatal(err)
	}
	c.Enable(ref)
	report, err := factory.Prepare(context.Background(), ref, spec)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Ready || string(report.CompiledBody) != string(spec.Candidate) {
		t.Fatal(report)
	}
	if rs := c.List(); len(rs) != 1 || rs[0].ActiveInstances != 0 {
		t.Fatal("preparation kept process references", rs)
	}
	if err = c.Disable(ref); err != nil {
		t.Fatal(err)
	}
	if err = c.Uninstall(ref); err != nil {
		t.Fatal(err)
	}
}
