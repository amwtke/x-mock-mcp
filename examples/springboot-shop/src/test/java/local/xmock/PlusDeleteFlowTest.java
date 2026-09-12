package local.xmock;

import org.junit.jupiter.api.Test;
import org.springframework.http.HttpMethod;
import org.springframework.test.context.ActiveProfiles;
import static org.junit.jupiter.api.Assertions.*;

@ActiveProfiles(value={"mock", "mybatis-plus"}, inheritProfiles=false)
class PlusDeleteFlowTest extends PlusShopFlowTest {
  @Override @Test void shoppingThroughRealHTTP() {
    super.shoppingThroughRealHTTP();
    assertEquals(404, call("/api/cart/items/5001", HttpMethod.DELETE, null, 2002).getStatusCode().value());
    assertEquals(2, call("/api/cart", HttpMethod.GET, null, 2001).getBody().get("items").get(0).get("quantity").asLong());
    assertEquals(204, call("/api/cart/items/5001", HttpMethod.DELETE, null, 2001).getStatusCode().value());
    var empty = call("/api/cart", HttpMethod.GET, null, 2001);
    assertEquals(0, empty.getBody().get("items").size());
    assertEquals(0, empty.getBody().get("totalCents").asLong());
    assertEquals(404, call("/api/cart/items/5001", HttpMethod.DELETE, null, 2001).getStatusCode().value());
    var product = call("/api/products/1001", HttpMethod.GET, null, 2001);
    assertEquals(200, product.getStatusCode().value());
    assertEquals(10, product.getBody().get("stock").asLong());
  }
}
