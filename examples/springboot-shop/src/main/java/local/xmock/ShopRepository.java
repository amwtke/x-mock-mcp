package local.xmock;
import java.sql.Statement;
import java.util.List;
import java.util.Optional;
import org.springframework.jdbc.core.JdbcTemplate;
import org.springframework.jdbc.core.RowMapper;
import org.springframework.jdbc.support.GeneratedKeyHolder;
import org.springframework.stereotype.Repository;
@Repository
public class ShopRepository {
 public record Product(long id,String name,long priceCents,long stock,String status){}
 public record CartItem(long id,long userId,long productId,long quantity){}
 private final JdbcTemplate jdbc;
 public ShopRepository(JdbcTemplate jdbc){this.jdbc=jdbc;}
 private final RowMapper<Product> products=(r,n)->new Product(r.getLong("id"),r.getString("name"),r.getLong("price_cents"),r.getLong("stock"),r.getString("status"));
 private final RowMapper<CartItem> cart=(r,n)->new CartItem(r.getLong("id"),r.getLong("user_id"),r.getLong("product_id"),r.getLong("quantity"));
 public List<Product> products(){return jdbc.query("SELECT id, name, price_cents, stock, status FROM products WHERE status = ? ORDER BY id",products,"ON_SALE");}
 public Optional<Product> product(long id){return jdbc.query("SELECT id, name, price_cents, stock, status FROM products WHERE id = ?",products,id).stream().findFirst();}
 public Optional<CartItem> item(long user,long product){return jdbc.query("SELECT id, user_id, product_id, quantity FROM cart_items WHERE user_id = ? AND product_id = ?",cart,user,product).stream().findFirst();}
 public List<CartItem> cart(long user){return jdbc.query("SELECT id, user_id, product_id, quantity FROM cart_items WHERE user_id = ? ORDER BY id",cart,user);}
 public long insert(long user,long product,long quantity){GeneratedKeyHolder key=new GeneratedKeyHolder();int changed=jdbc.update(connection->{var q=connection.prepareStatement("INSERT INTO cart_items (user_id, product_id, quantity) VALUES (?, ?, ?)",Statement.RETURN_GENERATED_KEYS);q.setLong(1,user);q.setLong(2,product);q.setLong(3,quantity);return q;},key);if(changed!=1||key.getKey()==null)throw new IllegalStateException("cart insert did not create a row");return key.getKey().longValue();}
 public void increment(long id,long user,long quantity){int changed=jdbc.update("UPDATE cart_items SET quantity = quantity + ? WHERE id = ? AND user_id = ?",quantity,id,user);if(changed!=1)throw new IllegalStateException("cart update did not affect one row");}
}
