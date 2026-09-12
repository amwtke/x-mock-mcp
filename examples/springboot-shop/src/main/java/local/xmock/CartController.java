package local.xmock;
import jakarta.servlet.http.HttpServletRequest;
import org.springframework.http.ResponseEntity;
import org.springframework.web.bind.annotation.*;
@RestController @RequestMapping("/api/cart")
public class CartController {
 public record AddRequest(long productId,long quantity){}
 private final CartService service;private final TestIdentity identity;
 public CartController(CartService service,TestIdentity identity){this.service=service;this.identity=identity;}
 @PostMapping("/items") public ResponseEntity<CartService.Added> add(@RequestBody AddRequest body,HttpServletRequest request){var result=service.add(identity.user(request),body.productId(),body.quantity());return ResponseEntity.status(result.created()?201:200).body(result);}
 @GetMapping public CartService.Cart cart(HttpServletRequest request){return service.cart(identity.user(request));}
}
