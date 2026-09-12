package local.xmock;

import local.xmock.plus.PlusShopRepository;
import org.junit.jupiter.api.Test;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.test.context.ActiveProfiles;
import static org.junit.jupiter.api.Assertions.*;

@ActiveProfiles(value={"mock", "mybatis-plus"}, inheritProfiles=false)
class PlusShoppingBrowserTest extends ShoppingBrowserTest {
  @Autowired ShopDataAccess repository;
  @Test void usesRealPlusRepository() { assertInstanceOf(PlusShopRepository.class, repository); }
}
