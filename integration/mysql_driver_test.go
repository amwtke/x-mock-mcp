package integration

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"xmock.local/x-mock-mcp/internal/binding"
	"xmock.local/x-mock-mcp/internal/plugin/catalog"
	pluginruntime "xmock.local/x-mock-mcp/internal/plugin/runtime"
	"xmock.local/x-mock-mcp/pluginapi"
)

func mysqlEnvironment(t *testing.T, fixture string) (*binding.Manager, binding.Config) {
	t.Helper()
	c, err := catalog.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	refs := map[pluginapi.Role]pluginapi.Reference{}
	for _, path := range []string{"./plugins/left/mysql-wire", "./plugins/right/mysql-mock"} {
		archive, digest, ref := packagePlugin(t, path, "")
		if _, err = c.Install(context.Background(), archive, digest); err != nil {
			t.Fatal(err)
		}
		if err = c.Enable(ref); err != nil {
			t.Fatal(err)
		}
		refs[ref.Role] = ref
	}
	factory := pluginruntime.New(c)
	root, _ := filepath.Abs("..")
	input, err := os.ReadFile(filepath.Join(root, "examples/scenarios/"+fixture+"-input.json"))
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := os.ReadFile(filepath.Join(root, "examples/scenarios/"+fixture+"-candidate.json"))
	if err != nil {
		t.Fatal(err)
	}
	report, err := factory.Prepare(context.Background(), refs[pluginapi.Right], pluginapi.PreparationSpec{ProjectRoot: root, Input: input, Candidate: candidate})
	if err != nil || !report.Ready {
		t.Fatalf("prepare: %v %+v", err, report)
	}
	m := binding.New(factory)
	t.Cleanup(func() {
		for _, b := range m.List() {
			m.Destroy(context.Background(), b.ID)
		}
	})
	conf := binding.Config{Left: refs[pluginapi.Left], Right: refs[pluginapi.Right], EnvironmentID: "jdbc", ResourceID: "app", Contract: pluginapi.Contract{ID: "mysql.operation", Version: 1}, LeftConfig: json.RawMessage(`{"host":"127.0.0.1","port":0,"username":"mock","password":"mock-local"}`), RightConfig: json.RawMessage(`{"mode":"StrictReplay"}`), Scenario: report.CompiledBody, ScenarioVersion: 1, RequestTimeoutMS: 10000, OutcomeObserver: func(q pluginapi.Request, raw json.RawMessage, err error) {
		if err != nil || strings.Contains(string(raw), `"kind":"error"`) {
			t.Logf("MySQL %s %s => %s %v", q.Operation, q.Payload, raw, err)
		}
	}}
	return m, conf
}
func runMaven(t *testing.T, klass, url string) {
	t.Helper()
	javaHome := os.Getenv("X_MOCK_JAVA_HOME")
	if javaHome == "" {
		javaHome = "/opt/homebrew/Cellar/openjdk@21/21.0.12/libexec/openjdk.jdk/Contents/Home"
	}
	if _, err := os.Stat(filepath.Join(javaHome, "bin/java")); err != nil {
		t.Fatal("set X_MOCK_JAVA_HOME to JDK 21", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "mvn", "-B", "-ntp", "-f", "examples/springboot-shop/pom.xml", "-Dtest="+klass, "test")
	cmd.Dir = ".."
	cmd.Env = append(os.Environ(), "JAVA_HOME="+javaHome, "X_MOCK_MYSQL_URL="+url)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Maven: %v\n%s", err, out)
	}
	t.Log(string(out))
}
func TestMySQLDriverContract(t *testing.T) {
	m, conf := mysqlEnvironment(t, "driver")
	for _, prepared := range []string{"true", "false"} {
		t.Run("serverPrepare="+prepared, func(t *testing.T) {
			b, err := m.Create(context.Background(), conf)
			if err != nil {
				t.Fatal(err)
			}
			defer m.Destroy(context.Background(), b.ID)
			runMaven(t, "DriverContractTest", "jdbc:mysql://"+b.Endpoints["mysql"]+"/app?sslMode=DISABLED&useServerPrepStmts="+prepared+"&emulateUnsupportedPstmts=false&cachePrepStmts=false&socketTimeout=10000&connectionCollation=utf8mb4_bin")
		})
	}
}
func TestMySQLTransactionContract(t *testing.T) {
	m, conf := mysqlEnvironment(t, "transaction")
	for _, affected := range []string{"false", "true"} {
		t.Run("useAffectedRows="+affected, func(t *testing.T) {
			b, err := m.Create(context.Background(), conf)
			if err != nil {
				t.Fatal(err)
			}
			defer m.Destroy(context.Background(), b.ID)
			runMaven(t, "TransactionContractTest", "jdbc:mysql://"+b.Endpoints["mysql"]+"/app?sslMode=DISABLED&useServerPrepStmts=true&emulateUnsupportedPstmts=false&cachePrepStmts=false&socketTimeout=10000&connectionCollation=utf8mb4_bin&useAffectedRows="+affected)
		})
	}
}
