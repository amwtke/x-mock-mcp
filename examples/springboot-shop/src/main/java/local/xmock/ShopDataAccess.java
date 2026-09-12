package local.xmock;

import java.util.List;
import java.util.Optional;

public interface ShopDataAccess {
  List<ShopRepository.Product> products();
  Optional<ShopRepository.Product> product(long id);
  Optional<ShopRepository.CartItem> item(long user, long product);
  List<ShopRepository.CartItem> cart(long user);
  long insert(long user, long product, long quantity);
  void increment(long id, long user, long quantity);
}
