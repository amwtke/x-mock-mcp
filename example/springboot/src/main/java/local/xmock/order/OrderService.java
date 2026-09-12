package local.xmock.order;

import java.util.List;
import org.springframework.http.HttpStatus;
import org.springframework.stereotype.Service;
import org.springframework.transaction.annotation.Isolation;
import org.springframework.transaction.annotation.Transactional;
import org.springframework.web.server.ResponseStatusException;

@Service
public class OrderService {
  private final OrderRepository repository;
  public OrderService(OrderRepository repository) { this.repository = repository; }
  public List<Product> products() { return repository.products(); }
  public Product product(long id) {
    return repository.product(id).orElseThrow(() -> new ResponseStatusException(HttpStatus.NOT_FOUND, "商品不存在"));
  }
  public List<PurchaseOrder> orders(long user) { return repository.orders(user); }
  public PurchaseOrder order(long id, long user) {
    return repository.order(id, user).orElseThrow(() -> new ResponseStatusException(HttpStatus.NOT_FOUND, "订单不存在"));
  }
  @Transactional(isolation=Isolation.READ_COMMITTED)
  public PurchaseOrder place(long user, long productId, long quantity) {
    if (quantity < 1 || quantity > 100) throw new ResponseStatusException(HttpStatus.BAD_REQUEST, "数量必须在1到100之间");
    Product product = product(productId);
    if (!"ON_SALE".equals(product.status())) throw new ResponseStatusException(HttpStatus.CONFLICT, "商品未上架");
    if (product.stock() < quantity) throw new ResponseStatusException(HttpStatus.CONFLICT, "库存不足");
    long total = Math.multiplyExact(product.priceCents(), quantity);
    if (!repository.reserveStock(product.id(), product.stock(), product.stock() - quantity))
      throw new ResponseStatusException(HttpStatus.CONFLICT, "库存已变化，请重试");
    long id = repository.insert(user, product, quantity, total);
    return repository.order(id, user).orElseThrow(() -> new IllegalStateException("写入后的订单不可见"));
  }
}
