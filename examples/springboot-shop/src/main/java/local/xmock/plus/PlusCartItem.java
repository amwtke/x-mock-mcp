package local.xmock.plus;

import com.baomidou.mybatisplus.annotation.*;

@TableName("cart_items")
public class PlusCartItem {
  @TableId(value="id", type=IdType.AUTO) private Long id;
  @TableField("user_id") private Long userId;
  @TableField("product_id") private Long productId;
  @TableField("quantity") private Long quantity;
  public Long getId() { return id; }
  public void setId(Long id) { this.id = id; }
  public Long getUserId() { return userId; }
  public void setUserId(Long userId) { this.userId = userId; }
  public Long getProductId() { return productId; }
  public void setProductId(Long productId) { this.productId = productId; }
  public Long getQuantity() { return quantity; }
  public void setQuantity(Long quantity) { this.quantity = quantity; }
}
