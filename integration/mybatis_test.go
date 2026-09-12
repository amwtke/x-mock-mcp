package integration

import (
	"context"
	"testing"
)

func TestMyBatisShoppingHTTPAndBrowser(t *testing.T) {
	m, conf := mysqlEnvironment(t, "mybatis")
	t.Setenv("PLAYWRIGHT_BROWSERS_PATH", browserHome())
	for _, klass := range []string{"MyBatisShopFlowTest", "MyBatisShoppingBrowserTest"} {
		t.Run(klass, func(t *testing.T) {
			b, err := m.Create(context.Background(), conf)
			if err != nil {
				t.Fatal(err)
			}
			defer m.Destroy(context.Background(), b.ID)
			// The endpoint is created by our left plugin, never an external database.
			runMaven(t, klass, "jdbc:mysql://"+b.Endpoints["mysql"]+"/app?sslMode=DISABLED&useServerPrepStmts=true&emulateUnsupportedPstmts=false&cachePrepStmts=false&socketTimeout=10000&connectionCollation=utf8mb4_bin")
		})
	}
}

func TestMyBatisAgentFlowAndReplay(t *testing.T) {
	shoppingAgentFlow(t, "mybatis", "MyBatisShoppingBrowserTest", "p1")
}

func TestMyBatisShoppingDefects(t *testing.T) {
	shoppingDefects(t, "mybatis", "MyBatisShopFlowTest", true)
}
