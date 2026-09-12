package local.xmock;

import java.io.InputStream;
import java.util.List;
import java.util.Map;
import org.apache.ibatis.builder.xml.XMLMapperBuilder;
import org.apache.ibatis.executor.keygen.Jdbc3KeyGenerator;
import org.apache.ibatis.mapping.ParameterMapping;
import org.apache.ibatis.session.Configuration;
import org.junit.jupiter.api.Test;
import static org.junit.jupiter.api.Assertions.*;

class MapperContractTest {
  @Test void realFrameworkRendersStaticStatementsWithoutADatabase() throws Exception {
    Configuration config = new Configuration();
    String resource = "mappers/ShopMapper.xml";
    try (InputStream stream = getClass().getClassLoader().getResourceAsStream(resource)) {
      assertNotNull(stream, "real shopping Mapper XML is required");
      new XMLMapperBuilder(stream, config, resource, config.getSqlFragments()).parse();
    }
    String namespace = "local.xmock.ShopMapper.";
    Map<String, List<String>> parameters = Map.of(
      "products", List.of("status"), "product", List.of("id"),
      "item", List.of("userId", "productId"), "cart", List.of("userId"),
      "insert", List.of("userId", "productId", "quantity"),
      "increment", List.of("quantity", "id", "userId"));
    Map<String, String> expectedSQL = Map.of(
      "products", "SELECT id, name, price_cents, stock, status FROM products WHERE status = ? ORDER BY id",
      "product", "SELECT id, name, price_cents, stock, status FROM products WHERE id = ?",
      "item", "SELECT id, user_id, product_id, quantity FROM cart_items WHERE user_id = ? AND product_id = ?",
      "cart", "SELECT id, user_id, product_id, quantity FROM cart_items WHERE user_id = ? ORDER BY id",
      "insert", "INSERT INTO cart_items (user_id, product_id, quantity) VALUES (?, ?, ?)",
      "increment", "UPDATE cart_items SET quantity = quantity + ? WHERE id = ? AND user_id = ?");
    for (var entry : parameters.entrySet()) {
      var statement = config.getMappedStatement(namespace + entry.getKey());
      var bound = statement.getBoundSql(Map.of());
      assertEquals(entry.getValue(), bound.getParameterMappings().stream().map(ParameterMapping::getProperty).toList());
      assertEquals(expectedSQL.get(entry.getKey()), bound.getSql().trim().replaceAll("\\s+", " "));
    }
    var insert = config.getMappedStatement(namespace + "insert");
    assertInstanceOf(Jdbc3KeyGenerator.class, insert.getKeyGenerator());
    assertArrayEquals(new String[]{"id"}, insert.getKeyProperties());
    assertEquals(ShopRepository.Product.class, config.getMappedStatement(namespace + "product").getResultMaps().getFirst().getType());
    assertEquals(ShopRepository.CartItem.class, config.getMappedStatement(namespace + "item").getResultMaps().getFirst().getType());
    assertNull(config.getEnvironment(), "SQL rendering must not need a database connection");
  }
}
