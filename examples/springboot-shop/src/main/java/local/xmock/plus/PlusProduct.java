package local.xmock.plus;

import com.baomidou.mybatisplus.annotation.*;

@TableName("products")
public class PlusProduct {
  @TableId(value="id", type=IdType.INPUT) private Long id;
  @TableField("name") private String name;
  @TableField("price_cents") private Long priceCents;
  @TableField("stock") private Long stock;
  @TableField("status") private String status;
  public Long getId() { return id; }
  public void setId(Long id) { this.id = id; }
  public String getName() { return name; }
  public void setName(String name) { this.name = name; }
  public Long getPriceCents() { return priceCents; }
  public void setPriceCents(Long priceCents) { this.priceCents = priceCents; }
  public Long getStock() { return stock; }
  public void setStock(Long stock) { this.stock = stock; }
  public String getStatus() { return status; }
  public void setStatus(String status) { this.status = status; }
}
