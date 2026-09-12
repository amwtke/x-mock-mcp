package main

import (
	"strings"
	"testing"
)

const plusCartJava = `package sample;
import com.baomidou.mybatisplus.annotation.TableName;
import com.baomidou.mybatisplus.annotation.TableId;
import com.baomidou.mybatisplus.annotation.TableField;
import com.baomidou.mybatisplus.annotation.IdType;
@TableName("cart_items") public class Cart {
 @TableId(value="id",type=IdType.AUTO) private Long id;
 @TableField("user_id") private Long userId;
 @TableField("quantity") private Long quantity;
 public Long getId(){return id;} public void setId(Long id){this.id=id;}
 public Long getUserId(){return userId;} public void setUserId(Long userId){this.userId=userId;}
 public Long getQuantity(){return quantity;} public void setQuantity(Long quantity){this.quantity=quantity;}
}`
const plusMapperJava = `package sample;
import com.baomidou.mybatisplus.core.mapper.BaseMapper;
public interface CartMapper extends BaseMapper<Cart> {}`

func TestPlusJavaEntityAndMapper(t *testing.T) {
	entity, err := parsePlusEntity([]byte(plusCartJava))
	if err != nil {
		t.Fatal(err)
	}
	if entity.Name != "sample.Cart" || entity.Table != "cart_items" || len(entity.Fields) != 3 || entity.Fields[1].Column != "user_id" || !entity.Fields[0].Auto {
		t.Fatalf("wrong entity mapping: %+v", entity)
	}
	if err = validatePlusMapper([]byte(plusMapperJava), "sample.CartMapper", entity.Name); err != nil {
		t.Fatal(err)
	}
}

func TestPlusJavaRejectsUnsupportedOrForgedMapping(t *testing.T) {
	for name, code := range map[string]string{
		"fake-annotation":      strings.ReplaceAll(plusCartJava, "com.baomidou.mybatisplus.annotation.TableName", "fake.TableName"),
		"fake-id-type":         strings.ReplaceAll(plusCartJava, "com.baomidou.mybatisplus.annotation.IdType", "fake.IdType"),
		"inheritance":          strings.ReplaceAll(plusCartJava, "class Cart {", "class Cart extends Parent {"),
		"logic-delete":         strings.ReplaceAll(plusCartJava, `@TableField("quantity")`, `@TableLogic @TableField("quantity")`),
		"field-fill":           strings.ReplaceAll(plusCartJava, `@TableField("quantity")`, `@TableField(value="quantity",fill=FieldFill.INSERT)`),
		"type-handler":         strings.ReplaceAll(plusCartJava, `@TableField("quantity")`, `@TableField(value="quantity",typeHandler=Custom.class)`),
		"missing-mapping":      strings.ReplaceAll(plusCartJava, `@TableField("quantity")`, ""),
		"unsupported-type":     strings.ReplaceAll(plusCartJava, "Long quantity", "Integer quantity"),
		"computed-getter":      strings.ReplaceAll(plusCartJava, "return userId;", "return 2002L;"),
		"duplicate-column":     strings.ReplaceAll(plusCartJava, `@TableField("quantity")`, `@TableField("user_id")`),
		"static-field":         strings.ReplaceAll(plusCartJava, "private Long quantity", "private static Long quantity"),
		"array-field":          strings.ReplaceAll(plusCartJava, "Long quantity;", "Long quantity[];"),
		"explicit-import-wins": strings.ReplaceAll(plusCartJava, "import com.baomidou.mybatisplus.annotation.TableName;", "import fake.TableName;\nimport com.baomidou.mybatisplus.annotation.*;"),
		"broken-java":          plusCartJava[:len(plusCartJava)-1],
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parsePlusEntity([]byte(code)); err == nil {
				t.Fatal("unsupported entity accepted")
			}
		})
	}
	for name, code := range map[string]string{
		"fake-base":        strings.ReplaceAll(plusMapperJava, "com.baomidou.mybatisplus.core.mapper.BaseMapper", "fake.BaseMapper"),
		"different-entity": strings.ReplaceAll(plusMapperJava, "BaseMapper<Cart>", "BaseMapper<Other>"),
		"override":         strings.ReplaceAll(plusMapperJava, "{}", "{ Cart selectById(Long id); }"),
		"indirect-base":    strings.ReplaceAll(plusMapperJava, "BaseMapper<Cart>", "CustomMapper<Cart>"),
	} {
		t.Run(name, func(t *testing.T) {
			if err := validatePlusMapper([]byte(code), "sample.CartMapper", "sample.Cart"); err == nil {
				t.Fatal("unsupported Mapper accepted")
			}
		})
	}
}
