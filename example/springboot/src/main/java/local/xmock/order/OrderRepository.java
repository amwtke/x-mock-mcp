package local.xmock.order;

import java.sql.Statement;
import java.util.List;
import java.util.Optional;
import org.springframework.jdbc.core.JdbcTemplate;
import org.springframework.jdbc.core.RowMapper;
import org.springframework.jdbc.support.GeneratedKeyHolder;
import org.springframework.stereotype.Repository;

@Repository
public class OrderRepository {
  private final JdbcTemplate jdbc;
  public OrderRepository(JdbcTemplate jdbc) { this.jdbc = jdbc; }
  private final RowMapper<Product> productRow = (r, n) -> new Product(r.getLong("id"), r.getString("name"), r.getLong("price_cents"), r.getLong("stock"), r.getString("status"));
  private final RowMapper<PurchaseOrder> orderRow = (r, n) -> new PurchaseOrder(r.getLong("id"), r.getLong("user_id"), r.getLong("product_id"), r.getString("product_name"), r.getLong("price_cents"), r.getLong("quantity"), r.getLong("total_cents"), r.getString("status"));

  public List<Product> products() {
    return jdbc.query("SELECT id, name, price_cents, stock, status FROM products WHERE status = ? ORDER BY id", productRow, "ON_SALE");
  }
  public Optional<Product> product(long id) {
    return jdbc.query("SELECT id, name, price_cents, stock, status FROM products WHERE id = ?", productRow, id).stream().findFirst();
  }
  public List<PurchaseOrder> orders(long user) {
    return jdbc.query("SELECT id, user_id, product_id, product_name, price_cents, quantity, total_cents, status FROM purchase_orders WHERE user_id = ? ORDER BY id", orderRow, user);
  }
  public Optional<PurchaseOrder> order(long id, long user) {
    return jdbc.query("SELECT id, user_id, product_id, product_name, price_cents, quantity, total_cents, status FROM purchase_orders WHERE id = ? AND user_id = ?", orderRow, id, user).stream().findFirst();
  }
  public boolean reserveStock(long id, long before, long after) {
    return jdbc.update("UPDATE products SET stock = ? WHERE id = ? AND stock = ?", after, id, before) == 1;
  }
  public long insert(long user, Product product, long quantity, long total) {
    GeneratedKeyHolder key = new GeneratedKeyHolder();
    int changed = jdbc.update(connection -> {
      var query = connection.prepareStatement("INSERT INTO purchase_orders (user_id, product_id, product_name, price_cents, quantity, total_cents, status) VALUES (?, ?, ?, ?, ?, ?, ?)", Statement.RETURN_GENERATED_KEYS);
      query.setLong(1, user); query.setLong(2, product.id()); query.setString(3, product.name());
      query.setLong(4, product.priceCents()); query.setLong(5, quantity); query.setLong(6, total);
      query.setString(7, "CREATED");
      return query;
    }, key);
    if (changed != 1 || key.getKey() == null) throw new IllegalStateException("订单写入未返回生成标识");
    return key.getKey().longValue();
  }
}
