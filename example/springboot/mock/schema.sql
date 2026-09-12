CREATE TABLE products (
  id BIGINT NOT NULL,
  name VARCHAR(128) NOT NULL,
  price_cents BIGINT NOT NULL,
  stock BIGINT NOT NULL,
  status VARCHAR(16) NOT NULL,
  PRIMARY KEY (id)
);
CREATE TABLE purchase_orders (
  id BIGINT NOT NULL AUTO_INCREMENT,
  user_id BIGINT NOT NULL,
  product_id BIGINT NOT NULL,
  product_name VARCHAR(128) NOT NULL,
  price_cents BIGINT NOT NULL,
  quantity BIGINT NOT NULL,
  total_cents BIGINT NOT NULL,
  status VARCHAR(16) NOT NULL,
  PRIMARY KEY (id),
  CONSTRAINT fk_order_product FOREIGN KEY (product_id) REFERENCES products (id)
);
