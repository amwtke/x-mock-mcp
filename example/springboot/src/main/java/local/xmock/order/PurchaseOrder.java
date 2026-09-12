package local.xmock.order;

public record PurchaseOrder(long id, long userId, long productId, String productName,
                            long priceCents, long quantity, long totalCents, String status) {}
