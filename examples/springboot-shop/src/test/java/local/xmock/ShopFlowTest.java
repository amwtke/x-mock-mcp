package local.xmock;
import com.fasterxml.jackson.databind.JsonNode;
import org.junit.jupiter.api.Test;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.test.context.SpringBootTest;
import org.springframework.boot.test.web.client.TestRestTemplate;
import org.springframework.http.*;
import org.springframework.test.context.ActiveProfiles;
import static org.junit.jupiter.api.Assertions.*;
@SpringBootTest(classes=ShopApplication.class,webEnvironment=SpringBootTest.WebEnvironment.RANDOM_PORT)
@ActiveProfiles("mock")
class ShopFlowTest {
 @Autowired TestRestTemplate http;
 ResponseEntity<JsonNode> call(String path,HttpMethod method,String body,long user){HttpHeaders headers=new HttpHeaders();headers.set("X-Test-User-Id",Long.toString(user));headers.setContentType(MediaType.APPLICATION_JSON);return http.exchange(path,method,new HttpEntity<>(body,headers),JsonNode.class);}
 @Test void shoppingThroughRealHTTP(){
  var list=call("/api/products",HttpMethod.GET,null,2001);assertEquals(200,list.getStatusCode().value());assertEquals("测试键盘",list.getBody().get(0).get("name").asText());assertEquals(9900,list.getBody().get(0).get("priceCents").asLong());
  var detail=call("/api/products/1001",HttpMethod.GET,null,2001);assertEquals(200,detail.getStatusCode().value());assertEquals(10,detail.getBody().get("stock").asLong());
  String input="{\"productId\":1001,\"quantity\":1}";
  var first=call("/api/cart/items",HttpMethod.POST,input,2001);assertEquals(201,first.getStatusCode().value());long key=first.getBody().get("cartItemId").asLong();assertEquals(5001,key);assertEquals(1,first.getBody().get("quantity").asLong());
  var cart=call("/api/cart",HttpMethod.GET,null,2001);assertEquals(1,cart.getBody().get("items").size());assertEquals(9900,cart.getBody().get("totalCents").asLong());
  var second=call("/api/cart/items",HttpMethod.POST,input,2001);assertEquals(200,second.getStatusCode().value());assertEquals(key,second.getBody().get("cartItemId").asLong());assertEquals(2,second.getBody().get("quantity").asLong());
  cart=call("/api/cart",HttpMethod.GET,null,2001);assertEquals(1,cart.getBody().get("items").size());assertEquals(19800,cart.getBody().get("totalCents").asLong());assertEquals(2,cart.getBody().get("items").get(0).get("quantity").asLong());
  var other=call("/api/cart",HttpMethod.GET,null,2002);assertEquals(0,other.getBody().get("items").size());assertEquals(0,other.getBody().get("totalCents").asLong());assertEquals(10,call("/api/products/1001",HttpMethod.GET,null,2001).getBody().get("stock").asLong());
 }
}
