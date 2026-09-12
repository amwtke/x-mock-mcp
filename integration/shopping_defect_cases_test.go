package integration

type shoppingDefect struct {
	name, source, before, after string
	prepareReject               bool
}

func shoppingDefectCases(orm, fixture string) []shoppingDefect {
	java := "examples/springboot-shop/src/main/java/local/xmock/"
	service := java + "CartService.java"
	plus := java + "plus/PlusShopRepository.java"
	if fixture == "plus-delete" {
		return []shoppingDefect{
			{"missing-delete", java + "plus/PlusCartController.java", "int changed = repository.removeForUser(id, identity.user(request));", "int changed = 1;", false},
			{"missing-delete-user-filter", plus, "return items.delete(new LambdaQueryWrapper<PlusCartItem>()\n      .eq(PlusCartItem::getId, id).eq(PlusCartItem::getUserId, user));", "return items.delete(new LambdaQueryWrapper<PlusCartItem>()\n      .eq(PlusCartItem::getId, id));", true},
		}
	}
	common := []shoppingDefect{
		{"missing-insert", service, "id=repository.insert(user,productId,quantity);", "id=5001L;", false},
		{"wrong-increment", service, "repository.increment(id,user,quantity);", "repository.increment(id,user,quantity+1);", false},
		{"forced-rollback", service, "return new Added(after.id()", "org.springframework.transaction.interceptor.TransactionAspectSupport.currentTransactionStatus().setRollbackOnly(); return new Added(after.id()", false},
	}
	specific := map[string][]shoppingDefect{
		"jdbc": {{"missing-user-filter", java + "ShopRepository.java", "FROM cart_items WHERE user_id = ? AND product_id = ?", "FROM cart_items WHERE product_id = ?", true}},
		"mybatis": {
			{"missing-user-filter", "examples/springboot-shop/src/main/resources/mappers/ShopMapper.xml", "WHERE user_id = #{userId,jdbcType=BIGINT} AND product_id = #{productId,jdbcType=BIGINT}", "WHERE product_id = #{productId,jdbcType=BIGINT}", true},
			{"swapped-mapper-arguments", java + "MyBatisShopRepository.java", "mapper.item(user, product)", "mapper.item(product, user)", false},
		},
		"plus": {
			{"missing-user-filter", plus, ".eq(PlusCartItem::getUserId, user).eq(PlusCartItem::getProductId, product)", ".eq(PlusCartItem::getProductId, product)", true},
			{"swapped-mapper-arguments", plus, ".eq(PlusCartItem::getUserId, user).eq(PlusCartItem::getProductId, product)", ".eq(PlusCartItem::getUserId, product).eq(PlusCartItem::getProductId, user)", false},
			{"wrong-entity-column", java + "plus/PlusProduct.java", "@TableField(\"price_cents\")", "@TableField(\"wrong_price\")", true},
		},
	}
	return append(common, specific[orm]...)
}
