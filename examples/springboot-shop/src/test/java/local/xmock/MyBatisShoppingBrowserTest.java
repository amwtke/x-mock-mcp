package local.xmock;

import org.junit.jupiter.api.Test;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.test.context.ActiveProfiles;
import static org.junit.jupiter.api.Assertions.*;

@ActiveProfiles(value={"mock", "mybatis"}, inheritProfiles=false)
class MyBatisShoppingBrowserTest extends ShoppingBrowserTest {
  @Autowired ShopDataAccess repository;
  @Test void usesRealMyBatisRepository() { assertInstanceOf(MyBatisShopRepository.class, repository); }
}
