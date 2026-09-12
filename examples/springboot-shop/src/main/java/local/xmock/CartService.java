package local.xmock;
import java.util.List;
import org.springframework.http.HttpStatus;
import org.springframework.stereotype.Service;
import org.springframework.transaction.annotation.Isolation;
import org.springframework.transaction.annotation.Transactional;
import org.springframework.web.server.ResponseStatusException;
@Service
public class CartService {
 public record Added(long cartItemId,long productId,long quantity,boolean created){}
 public record Line(long cartItemId,long productId,String name,long quantity,long priceCents){}
 public record Cart(List<Line> items,long totalCents){}
 private final ShopRepository repository;
 public CartService(ShopRepository repository){this.repository=repository;}
 @Transactional(isolation=Isolation.READ_COMMITTED)
 public Added add(long user,long productId,long quantity){
  if(quantity<1)throw new ResponseStatusException(HttpStatus.BAD_REQUEST,"quantity must be positive");
  var product=repository.product(productId).orElseThrow(()->new ResponseStatusException(HttpStatus.NOT_FOUND));
  if(!product.status().equals("ON_SALE"))throw new ResponseStatusException(HttpStatus.CONFLICT,"not on sale");
  var before=repository.item(user,productId);boolean created=before.isEmpty();long id;
  if(created){id=repository.insert(user,productId,quantity);}else{id=before.get().id();repository.increment(id,user,quantity);}
  var after=repository.item(user,productId).orElseThrow(()->new IllegalStateException("write was not visible"));
  if(after.id()!=id)throw new IllegalStateException("cart identity changed");
  return new Added(after.id(),after.productId(),after.quantity(),created);
 }
 public Cart cart(long user){var items=repository.cart(user).stream().map(item->{var product=repository.product(item.productId()).orElseThrow();return new Line(item.id(),item.productId(),product.name(),item.quantity(),product.priceCents());}).toList();long total=0;for(var item:items)total=Math.addExact(total,Math.multiplyExact(item.quantity(),item.priceCents()));return new Cart(items,total);}
}
