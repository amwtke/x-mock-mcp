package integration

import (
	"context"
	"testing"
)

func TestMyBatisPlusShoppingHTTPBrowserAndDelete(t *testing.T) {
	t.Setenv("PLAYWRIGHT_BROWSERS_PATH", browserHome())
	for _, tc := range []struct{ fixture, klass string }{
		{"plus", "PlusShopFlowTest"}, {"plus", "PlusShoppingBrowserTest"}, {"plus-delete", "PlusDeleteFlowTest"},
	} {
		t.Run(tc.klass, func(t *testing.T) {
			manager, config := mysqlEnvironment(t, tc.fixture)
			environment, err := manager.Create(context.Background(), config)
			if err != nil {
				t.Fatal(err)
			}
			defer manager.Destroy(context.Background(), environment.ID)
			runMaven(t, tc.klass, "jdbc:mysql://"+environment.Endpoints["mysql"]+"/app?sslMode=DISABLED&useServerPrepStmts=true&emulateUnsupportedPstmts=false&cachePrepStmts=false&socketTimeout=10000&connectionCollation=utf8mb4_bin")
		})
	}
}

func TestMyBatisPlusAgentFlowAndReplay(t *testing.T) {
	shoppingAgentFlow(t, "plus", "PlusShoppingBrowserTest", "p1-plus")
}

func TestMyBatisPlusShoppingDefects(t *testing.T) {
	shoppingDefects(t, "plus", "PlusShopFlowTest", "plus")
}

func TestMyBatisPlusDeleteDefects(t *testing.T) {
	shoppingDefects(t, "plus-delete", "PlusDeleteFlowTest", "plus")
}
