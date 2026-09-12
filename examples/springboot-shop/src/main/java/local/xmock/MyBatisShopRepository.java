package local.xmock;

import java.util.List;
import java.util.Optional;
import org.springframework.context.annotation.Profile;
import org.springframework.stereotype.Repository;

@Repository
@Profile("mybatis")
public class MyBatisShopRepository implements ShopDataAccess {
  private final ShopMapper mapper;
  public MyBatisShopRepository(ShopMapper mapper) { this.mapper = mapper; }
  public List<ShopRepository.Product> products() { return mapper.products("ON_SALE"); }
  public Optional<ShopRepository.Product> product(long id) { return Optional.ofNullable(mapper.product(id)); }
  public Optional<ShopRepository.CartItem> item(long user, long product) { return Optional.ofNullable(mapper.item(user, product)); }
  public List<ShopRepository.CartItem> cart(long user) { return mapper.cart(user); }
  public long insert(long user, long product, long quantity) {
    CartInsert row = new CartInsert(user, product, quantity);
    int changed = mapper.insert(row);
    if (changed != 1 || row.getId() == null) throw new IllegalStateException("cart insert did not create a row");
    return row.getId();
  }
  public void increment(long id, long user, long quantity) {
    if (mapper.increment(quantity, id, user) != 1) throw new IllegalStateException("cart update did not affect one row");
  }
}
