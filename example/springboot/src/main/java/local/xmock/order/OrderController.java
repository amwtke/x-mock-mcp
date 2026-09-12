package local.xmock.order;

import java.util.List;
import org.springframework.http.HttpStatus;
import org.springframework.http.ResponseEntity;
import org.springframework.web.bind.annotation.*;
import org.springframework.web.server.ResponseStatusException;

@RestController
@RequestMapping("/api")
public class OrderController {
  public record OrderRequest(long productId, long quantity) {}
  private final OrderService service;
  public OrderController(OrderService service) { this.service = service; }
  private long user(String identity) {
    if ("2001".equals(identity)) return 2001;
    if ("2002".equals(identity)) return 2002;
    throw new ResponseStatusException(HttpStatus.UNAUTHORIZED, "请提供有效的测试用户身份");
  }
  @GetMapping("/products") public List<Product> products() { return service.products(); }
  @GetMapping("/products/{id}") public Product product(@PathVariable long id) { return service.product(id); }
  @PostMapping("/orders") public ResponseEntity<PurchaseOrder> place(@RequestBody OrderRequest request,
      @RequestHeader(value="X-Test-User-Id", required=false) String identity) {
    return ResponseEntity.status(HttpStatus.CREATED).body(service.place(user(identity), request.productId(), request.quantity()));
  }
  @GetMapping("/orders") public List<PurchaseOrder> orders(@RequestHeader(value="X-Test-User-Id", required=false) String identity) {
    return service.orders(user(identity));
  }
  @GetMapping("/orders/{id}") public PurchaseOrder order(@PathVariable long id,
      @RequestHeader(value="X-Test-User-Id", required=false) String identity) {
    return service.order(id, user(identity));
  }
}
