package local.xmock;

public class CartInsert {
  private Long id;
  private final long userId;
  private final long productId;
  private final long quantity;
  public CartInsert(long userId, long productId, long quantity) {
    this.userId = userId; this.productId = productId; this.quantity = quantity;
  }
  public Long getId() { return id; }
  public void setId(Long id) { this.id = id; }
  public long getUserId() { return userId; }
  public long getProductId() { return productId; }
  public long getQuantity() { return quantity; }
}
