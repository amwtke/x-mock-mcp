package integration

import (
	"context"
	"testing"
)

func TestShoppingHTTPAndBrowser(t *testing.T) {
	m, conf := mysqlEnvironment(t, "shop")
	t.Setenv("PLAYWRIGHT_BROWSERS_PATH", browserHome())
	for _, klass := range []string{"ShopFlowTest", "ShoppingBrowserTest"} {
		t.Run(klass, func(t *testing.T) {
			b, err := m.Create(context.Background(), conf)
			if err != nil {
				t.Fatal(err)
			}
			defer m.Destroy(context.Background(), b.ID)
			runMaven(t, klass, "jdbc:mysql://"+b.Endpoints["mysql"]+"/app?sslMode=DISABLED&useServerPrepStmts=true&emulateUnsupportedPstmts=false&cachePrepStmts=false&socketTimeout=10000&connectionCollation=utf8mb4_bin")
		})
	}
}
