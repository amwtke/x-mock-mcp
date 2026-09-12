package local.xmock.plus;

import com.baomidou.mybatisplus.core.conditions.query.LambdaQueryWrapper;
import java.util.List;
import java.util.Optional;
import local.xmock.ShopDataAccess;
import local.xmock.ShopRepository;
import org.springframework.context.annotation.Profile;
import org.springframework.stereotype.Repository;

@Repository
@Profile("mybatis-plus")
public class PlusShopRepository implements ShopDataAccess {
  private final ProductMapper products;
  private final CartMapper items;
  public PlusShopRepository(ProductMapper products, CartMapper items) {
    this.products = products;
    this.items = items;
  }
  public List<ShopRepository.Product> products() {
    return products.selectList(new LambdaQueryWrapper<PlusProduct>()
      .eq(PlusProduct::getStatus, "ON_SALE").orderByAsc(PlusProduct::getId))
      .stream().map(PlusShopRepository::productView).toList();
  }
  public Optional<ShopRepository.Product> product(long id) {
    return Optional.ofNullable(products.selectById(id)).map(PlusShopRepository::productView);
  }
  public Optional<ShopRepository.CartItem> item(long user, long product) {
    return Optional.ofNullable(items.selectOne(new LambdaQueryWrapper<PlusCartItem>()
      .eq(PlusCartItem::getUserId, user).eq(PlusCartItem::getProductId, product)))
      .map(PlusShopRepository::cartView);
  }
  public List<ShopRepository.CartItem> cart(long user) {
    return items.selectList(new LambdaQueryWrapper<PlusCartItem>()
      .eq(PlusCartItem::getUserId, user).orderByAsc(PlusCartItem::getId))
      .stream().map(PlusShopRepository::cartView).toList();
  }
  public long insert(long user, long product, long quantity) {
    PlusCartItem row = new PlusCartItem();
    row.setUserId(user);
    row.setProductId(product);
    row.setQuantity(quantity);
    int changed = items.insert(row);
    if (changed != 1 || row.getId() == null) throw new IllegalStateException("cart insert did not create a row");
    return row.getId();
  }
  public void increment(long id, long user, long quantity) {
    PlusCartItem row = items.selectOne(new LambdaQueryWrapper<PlusCartItem>()
      .eq(PlusCartItem::getId, id).eq(PlusCartItem::getUserId, user));
    if (row == null) throw new IllegalStateException("cart item not found for user");
    row.setQuantity(Math.addExact(row.getQuantity(), quantity));
    int changed = items.updateById(row);
    if (changed != 1) throw new IllegalStateException("cart update did not affect one row");
  }
  public int removeForUser(long id, long user) {
    return items.delete(new LambdaQueryWrapper<PlusCartItem>()
      .eq(PlusCartItem::getId, id).eq(PlusCartItem::getUserId, user));
  }
  private static ShopRepository.Product productView(PlusProduct row) {
    return new ShopRepository.Product(row.getId(), row.getName(), row.getPriceCents(), row.getStock(), row.getStatus());
  }
  private static ShopRepository.CartItem cartView(PlusCartItem row) {
    return new ShopRepository.CartItem(row.getId(), row.getUserId(), row.getProductId(), row.getQuantity());
  }
}
