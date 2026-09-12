package local.xmock;

import java.util.List;
import org.apache.ibatis.annotations.Mapper;
import org.apache.ibatis.annotations.Param;
import org.springframework.context.annotation.Profile;

@Mapper
@Profile("mybatis")
public interface ShopMapper {
  List<ShopRepository.Product> products(@Param("status") String status);
  ShopRepository.Product product(@Param("id") long id);
  ShopRepository.CartItem item(@Param("userId") long userId, @Param("productId") long productId);
  List<ShopRepository.CartItem> cart(@Param("userId") long userId);
  int insert(CartInsert row);
  int increment(@Param("quantity") long quantity, @Param("id") long id, @Param("userId") long userId);
}
