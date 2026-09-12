package local.xmock;

import com.baomidou.mybatisplus.core.MybatisConfiguration;
import com.baomidou.mybatisplus.core.conditions.query.LambdaQueryWrapper;
import com.fasterxml.jackson.databind.ObjectMapper;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.List;
import java.util.Map;
import local.xmock.plus.*;
import org.apache.ibatis.executor.keygen.Jdbc3KeyGenerator;
import org.apache.ibatis.mapping.ParameterMapping;
import org.junit.jupiter.api.Test;
import static org.junit.jupiter.api.Assertions.*;

class PlusMapperContractTest {
  record Captured(String id, String sql, List<String> parameters) {}
  private static String compact(String sql) { return sql.replaceAll("\\s+", "").toLowerCase(); }
  private static void capture(MybatisConfiguration config, List<Captured> output, String id,
      Class<?> mapper, String method, Object parameters, String expectedSQL, String... names) {
    var statement = config.getMappedStatement(mapper.getName() + "." + method);
    var bound = statement.getBoundSql(parameters);
    assertEquals(compact(expectedSQL), compact(bound.getSql()), id);
    var actualNames = bound.getParameterMappings().stream().map(ParameterMapping::getProperty).toList();
    assertEquals(List.of(names), actualNames, id);
    output.add(new Captured(id, bound.getSql().trim().replaceAll("\\s+", " "), actualNames));
  }
  @Test void realFrameworkGeneratesCRUDWithoutADatabase() throws Exception {
    MybatisConfiguration config = new MybatisConfiguration();
    config.setMapUnderscoreToCamelCase(true);
    config.setCacheEnabled(false);
    config.addMapper(ProductMapper.class);
    config.addMapper(CartMapper.class);
    List<Captured> output = new ArrayList<>();
    String product = "SELECT id,name,price_cents,stock,status FROM products";
    String cart = "SELECT id,user_id,product_id,quantity FROM cart_items";
    String p1 = "ew.paramNameValuePairs.MPGENVAL1", p2 = "ew.paramNameValuePairs.MPGENVAL2";
    capture(config, output, "list-products", ProductMapper.class, "selectList",
      Map.of("ew", new LambdaQueryWrapper<PlusProduct>().eq(PlusProduct::getStatus, "ON_SALE").orderByAsc(PlusProduct::getId)),
      product + " WHERE (status = ?) ORDER BY id ASC", p1);
    capture(config, output, "product-detail", ProductMapper.class, "selectById", Map.of("id", 1001L),
      product + " WHERE id=?", "id");
    // BaseMapper.selectOne delegates to selectList; there is no synthetic selectOne MappedStatement.
    capture(config, output, "find-cart-item", CartMapper.class, "selectList",
      Map.of("ew", new LambdaQueryWrapper<PlusCartItem>().eq(PlusCartItem::getUserId, 2001L).eq(PlusCartItem::getProductId, 1001L)),
      cart + " WHERE (user_id=? AND product_id=?)", p1, p2);
    capture(config, output, "list-cart", CartMapper.class, "selectList",
      Map.of("ew", new LambdaQueryWrapper<PlusCartItem>().eq(PlusCartItem::getUserId, 2001L).orderByAsc(PlusCartItem::getId)),
      cart + " WHERE (user_id=?) ORDER BY id ASC", p1);
    PlusCartItem row = new PlusCartItem();
    row.setUserId(2001L); row.setProductId(1001L); row.setQuantity(1L);
    capture(config, output, "insert-cart", CartMapper.class, "insert", row,
      "INSERT INTO cart_items (user_id,product_id,quantity) VALUES (?,?,?)", "userId", "productId", "quantity");
    row.setId(5001L); row.setQuantity(2L);
    capture(config, output, "cart-by-user-id", CartMapper.class, "selectList",
      Map.of("ew", new LambdaQueryWrapper<PlusCartItem>().eq(PlusCartItem::getId, 5001L).eq(PlusCartItem::getUserId, 2001L)),
      cart + " WHERE (id=? AND user_id=?)", p1, p2);
    capture(config, output, "increment-cart", CartMapper.class, "updateById", Map.of("et", row),
      "UPDATE cart_items SET user_id=?,product_id=?,quantity=? WHERE id=?", "et.userId", "et.productId", "et.quantity", "et.id");
    capture(config, output, "delete-cart", CartMapper.class, "delete",
      Map.of("ew", new LambdaQueryWrapper<PlusCartItem>().eq(PlusCartItem::getId, 5001L).eq(PlusCartItem::getUserId, 2001L)),
      "DELETE FROM cart_items WHERE (id=? AND user_id=?)", p1, p2);
    capture(config, output, "delete-by-id", CartMapper.class, "deleteById", row,
      "DELETE FROM cart_items WHERE id=?", "id");
    var insert = config.getMappedStatement(CartMapper.class.getName() + ".insert");
    assertInstanceOf(Jdbc3KeyGenerator.class, insert.getKeyGenerator());
    assertArrayEquals(new String[]{"id"}, insert.getKeyProperties());
    assertEquals(PlusProduct.class, config.getMappedStatement(ProductMapper.class.getName() + ".selectById").getResultMaps().getFirst().getType());
    assertNull(config.getEnvironment(), "offline rendering must not create a DataSource");
    Files.createDirectories(Path.of("target"));
    new ObjectMapper().writerWithDefaultPrettyPrinter().writeValue(Path.of("target/plus-statements.json").toFile(), output);
  }
}
