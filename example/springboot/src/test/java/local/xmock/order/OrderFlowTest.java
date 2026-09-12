package local.xmock.order;

import com.fasterxml.jackson.databind.JsonNode;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.AfterEach;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.test.context.SpringBootTest;
import org.springframework.boot.test.web.client.TestRestTemplate;
import org.springframework.http.*;
import static org.junit.jupiter.api.Assertions.*;

@SpringBootTest(classes=OrderApplication.class, webEnvironment=SpringBootTest.WebEnvironment.RANDOM_PORT)
class OrderFlowTest {
  @Autowired TestRestTemplate http;
  ResponseEntity<JsonNode> call(String path, HttpMethod method, String body, long user) {
    HttpHeaders headers = new HttpHeaders();
    if (user != 0) headers.set("X-Test-User-Id", Long.toString(user));
    headers.setContentType(MediaType.APPLICATION_JSON);
    return http.exchange(path, method, new HttpEntity<>(body, headers), JsonNode.class);
  }
  @AfterEach void auditStateAfterInjectedDefect() {
    String expectedOrders = System.getProperty("xmock.failure.expectedOrders");
    if (expectedOrders == null) return;
    // The integration harness mutates an isolated source copy. Inspect state
    // before Maven exits and the failed run closes its protocol bindings.
    var product = call("/api/products/1001", HttpMethod.GET, null, 2001);
    var orders = call("/api/orders", HttpMethod.GET, null, 2001);
    assertEquals(200, product.getStatusCode().value());
    assertEquals(200, orders.getStatusCode().value());
    assertEquals(10, product.getBody().get("stock").asLong());
    assertEquals(Integer.parseInt(expectedOrders), orders.getBody().size());
    System.out.println("Defect-state audit passed: stock=10 orders=" + expectedOrders);
  }
  @Test void queryPlaceOrderAndReadBackThroughRealHTTP() {
    var products = call("/api/products", HttpMethod.GET, null, 2001);
    assertEquals(200, products.getStatusCode().value());
    assertEquals(1, products.getBody().size());
    assertEquals("测试键盘", products.getBody().get(0).get("name").asText());
    assertEquals(9900, products.getBody().get(0).get("priceCents").asLong());
    assertEquals(10, call("/api/products/1001", HttpMethod.GET, null, 2001).getBody().get("stock").asLong());
    assertEquals(0, call("/api/orders", HttpMethod.GET, null, 2001).getBody().size());
    assertEquals(404, call("/api/products/9999", HttpMethod.GET, null, 2001).getStatusCode().value());
    assertEquals(401, call("/api/orders", HttpMethod.GET, null, 0).getStatusCode().value());
    assertEquals(400, call("/api/orders", HttpMethod.POST, "{\"productId\":1001,\"quantity\":0}", 2001).getStatusCode().value());
    var placed = call("/api/orders", HttpMethod.POST, "{\"productId\":1001,\"quantity\":2}", 2001);
    assertEquals(201, placed.getStatusCode().value());
    assertEquals(9001, placed.getBody().get("id").asLong());
    assertEquals(2001, placed.getBody().get("userId").asLong());
    assertEquals(1001, placed.getBody().get("productId").asLong());
    assertEquals("测试键盘", placed.getBody().get("productName").asText());
    assertEquals(2, placed.getBody().get("quantity").asLong());
    assertEquals(19800, placed.getBody().get("totalCents").asLong());
    assertEquals("CREATED", placed.getBody().get("status").asText());
    var saved = call("/api/orders/9001", HttpMethod.GET, null, 2001);
    assertEquals(200, saved.getStatusCode().value());
    assertEquals(placed.getBody(), saved.getBody());
    assertEquals(8, call("/api/products/1001", HttpMethod.GET, null, 2001).getBody().get("stock").asLong());
    assertEquals(1, call("/api/orders", HttpMethod.GET, null, 2001).getBody().size());
    assertEquals(404, call("/api/orders/9001", HttpMethod.GET, null, 2002).getStatusCode().value());
    assertEquals(0, call("/api/orders", HttpMethod.GET, null, 2002).getBody().size());
    assertEquals(409, call("/api/orders", HttpMethod.POST, "{\"productId\":1001,\"quantity\":99}", 2001).getStatusCode().value());
    assertEquals(404, call("/api/orders", HttpMethod.POST, "{\"productId\":9999,\"quantity\":1}", 2001).getStatusCode().value());
    assertEquals(8, call("/api/products/1001", HttpMethod.GET, null, 2001).getBody().get("stock").asLong());
    assertEquals(1, call("/api/orders", HttpMethod.GET, null, 2001).getBody().size());
  }
}
