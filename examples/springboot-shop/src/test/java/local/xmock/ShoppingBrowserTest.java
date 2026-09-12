package local.xmock;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.microsoft.playwright.*;
import com.microsoft.playwright.options.AriaRole;
import java.nio.file.Path;
import java.util.Map;
import org.junit.jupiter.api.Test;
import org.springframework.boot.test.context.SpringBootTest;
import org.springframework.boot.test.web.server.LocalServerPort;
import org.springframework.test.context.ActiveProfiles;
import static org.junit.jupiter.api.Assertions.*;
import static com.microsoft.playwright.assertions.PlaywrightAssertions.assertThat;
@SpringBootTest(classes=ShopApplication.class,webEnvironment=SpringBootTest.WebEnvironment.RANDOM_PORT)
@ActiveProfiles("mock")
class ShoppingBrowserTest {
 @LocalServerPort int port;
 @Test void actualShoppingClicks()throws Exception{
	  com.microsoft.playwright.assertions.PlaywrightAssertions.setDefaultAssertionTimeout(180000);
  String run=System.getenv().getOrDefault("X_MOCK_RUN_ID","manual");Path artifacts=Path.of("target/browser",run);java.nio.file.Files.createDirectories(artifacts);
  try(Playwright pw=Playwright.create();Browser browser=pw.chromium().launch(new BrowserType.LaunchOptions().setHeadless(true));BrowserContext u1=browser.newContext(new Browser.NewContextOptions().setExtraHTTPHeaders(Map.of("X-Test-User-Id","2001")))){
   u1.setDefaultTimeout(180000);u1.tracing().start(new Tracing.StartOptions().setScreenshots(true).setSnapshots(true));Page page=u1.newPage();boolean failed=true;
   try{
    page.navigate("http://127.0.0.1:"+port+"/");
    assertThat(page.getByRole(AriaRole.LINK,new Page.GetByRoleOptions().setName("测试键盘"))).isVisible();assertThat(page.getByTestId("product-price")).hasText("99.00 元");
    page.getByRole(AriaRole.LINK,new Page.GetByRoleOptions().setName("测试键盘")).click();assertThat(page.getByTestId("stock")).hasText("库存 10");assertThat(page.getByTestId("detail-price")).hasText("99.00 元");page.getByLabel("数量").fill("1");
    Response first=page.waitForResponse(r->r.url().endsWith("/api/cart/items")&&r.request().method().equals("POST"),()->page.getByRole(AriaRole.BUTTON,new Page.GetByRoleOptions().setName("加入购物车")).click());assertEquals(201,first.status());long id=new ObjectMapper().readTree(first.text()).get("cartItemId").asLong();assertEquals(5001,id);assertThat(page.getByRole(AriaRole.STATUS)).hasText("加入成功");
    page.getByRole(AriaRole.BUTTON,new Page.GetByRoleOptions().setName("购物车").setExact(true)).click();assertThat(page.getByTestId("cart-item")).hasCount(1);assertThat(page.getByTestId("cart-quantity")).hasText("1");assertThat(page.getByTestId("total")).hasText("99.00 元");
    page.getByRole(AriaRole.LINK,new Page.GetByRoleOptions().setName("测试键盘")).click();page.getByLabel("数量").fill("1");Response second=page.waitForResponse(r->r.url().endsWith("/api/cart/items")&&r.request().method().equals("POST"),()->page.getByRole(AriaRole.BUTTON,new Page.GetByRoleOptions().setName("加入购物车")).click());assertEquals(200,second.status());assertEquals(id,new ObjectMapper().readTree(second.text()).get("cartItemId").asLong());
    page.getByRole(AriaRole.BUTTON,new Page.GetByRoleOptions().setName("购物车").setExact(true)).click();assertThat(page.getByTestId("cart-item")).hasCount(1);assertThat(page.getByTestId("cart-quantity")).hasText("2");assertThat(page.getByTestId("total")).hasText("198.00 元");assertThat(page.getByTestId("cart-id")).hasText(Long.toString(id));
    try(BrowserContext u2=browser.newContext(new Browser.NewContextOptions().setExtraHTTPHeaders(Map.of("X-Test-User-Id","2002")))){Page other=u2.newPage();other.navigate("http://127.0.0.1:"+port+"/");other.getByRole(AriaRole.BUTTON,new Page.GetByRoleOptions().setName("购物车").setExact(true)).click();assertThat(other.getByTestId("empty-cart")).hasText("购物车为空");assertThat(other.getByTestId("cart-item")).hasCount(0);}
    failed=false;page.screenshot(new Page.ScreenshotOptions().setPath(artifacts.resolve("completed.png")));
   }finally{if(failed)page.screenshot(new Page.ScreenshotOptions().setPath(artifacts.resolve("failure.png")));u1.tracing().stop(new Tracing.StopOptions().setPath(artifacts.resolve("trace.zip")));}
  }
 }
}
