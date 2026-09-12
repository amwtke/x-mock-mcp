package local.xmock.order;

import com.microsoft.playwright.*;
import com.microsoft.playwright.options.AriaRole;
import java.nio.file.Files;
import java.nio.file.Path;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.Timeout;
import org.springframework.boot.test.context.SpringBootTest;
import org.springframework.boot.test.web.server.LocalServerPort;
import static com.microsoft.playwright.assertions.PlaywrightAssertions.assertThat;
import static org.junit.jupiter.api.Assertions.assertEquals;

@SpringBootTest(classes=OrderApplication.class, webEnvironment=SpringBootTest.WebEnvironment.RANDOM_PORT)
class OrderBrowserTest {
  @LocalServerPort int port;
  @Test @Timeout(90) void browseOrderAndSeeReducedStock() throws Exception {
    String run = System.getenv().getOrDefault("X_MOCK_RUN_ID", "local");
    Path artifacts = Path.of("target/browser", run);
    Files.createDirectories(artifacts);
    try (Playwright playwright = Playwright.create(); Browser browser = playwright.chromium().launch()) {
      BrowserContext context = browser.newContext();
      context.tracing().start(new Tracing.StartOptions().setScreenshots(true).setSnapshots(true));
      Page page = context.newPage();
      try {
        page.navigate("http://127.0.0.1:" + port);
        assertThat(page.getByTestId("product-list")).containsText("测试键盘");
        assertThat(page.getByTestId("product-list")).containsText("99.00");
        page.getByRole(AriaRole.BUTTON, new Page.GetByRoleOptions().setName("查看详情")).click();
        assertThat(page.getByTestId("stock")).hasText("10");
        page.getByLabel("购买数量").fill("2");
        Response response = page.waitForResponse(r -> r.url().endsWith("/api/orders") && r.request().method().equals("POST"),
          () -> page.getByRole(AriaRole.BUTTON, new Page.GetByRoleOptions().setName("下单").setExact(true)).click());
        assertEquals(201, response.status());
        assertThat(page.getByTestId("receipt")).containsText("9001");
        assertThat(page.getByTestId("receipt")).containsText("198.00");
        assertThat(page.getByTestId("stock")).hasText("8");
        page.getByRole(AriaRole.BUTTON, new Page.GetByRoleOptions().setName("查询我的订单")).click();
        assertThat(page.getByTestId("order-list")).containsText("9001");
        assertThat(page.getByTestId("order-list")).containsText("2 件");
        page.screenshot(new Page.ScreenshotOptions().setPath(artifacts.resolve("order.png")).setFullPage(true));
      } finally {
        context.tracing().stop(new Tracing.StopOptions().setPath(artifacts.resolve("trace.zip")));
        context.close();
      }
    }
  }
}
