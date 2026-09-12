package main

import (
	"context"
	"sort"
	"strings"
	"testing"
	"xmock.local/x-mock-mcp/contracts/mysqlv1"
)

func TestPlusPrepareRevalidatesActualSourceHashes(t *testing.T) {
	spec, in, body := preparationFixture(t)
	s, files := plusSourceFixture("selectById", "detail", "SELECT id,user_id,quantity FROM cart_items WHERE id=?", "id")
	s.ID = "product"
	in.Sources = in.Sources[:1]
	paths := []string{}
	for path := range files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		in.Sources = append(in.Sources, sourceFile(t, spec.ProjectRoot, path, "code", string(files[path])))
	}
	body.Evidence = in.Sources
	body.DatabaseScenario.Statements = []Statement{s}
	spec.Input, spec.Candidate = mysqlv1.Encode(in), mysqlv1.Encode(body)
	report, err := (&plugin{}).Prepare(context.Background(), spec)
	if err != nil || !report.Ready {
		t.Fatalf("actual Plus evidence rejected: %+v %v", report, err)
	}
	changed := strings.ReplaceAll(plusCartJava, `@TableName("cart_items")`, `@TableName("other")`)
	for i, source := range in.Sources {
		if source.Path == "Cart.java" {
			in.Sources[i] = sourceFile(t, spec.ProjectRoot, "Cart.java", "code", changed)
		}
	}
	body.Evidence = in.Sources
	spec.Input, spec.Candidate = mysqlv1.Encode(in), mysqlv1.Encode(body)
	report, err = (&plugin{}).Prepare(context.Background(), spec)
	if err != nil || report.Ready {
		t.Fatal("changed table accepted after refreshed SHA", err)
	}
	if len(report.Diagnostics) == 0 || strings.Contains(report.Diagnostics[0].Message, "STALE_EVIDENCE") {
		t.Fatal("source semantics not checked", report)
	}
}

const plusRepositoryJava = `package sample;
import com.baomidou.mybatisplus.core.conditions.query.LambdaQueryWrapper;
public class Repository {
 private final CartMapper carts;
 public Repository(CartMapper carts){this.carts=carts;}
 public Cart detail(long id){return carts.selectById(id);}
 public Object list(long user){return carts.selectList(new LambdaQueryWrapper<Cart>().eq(Cart::getUserId,user).orderByAsc(Cart::getId));}
 public Cart item(long user,long id){return carts.selectOne(new LambdaQueryWrapper<Cart>().eq(Cart::getUserId,user).eq(Cart::getId,id));}
 public int insert(Cart row){return carts.insert(row);}
 public int update(Cart row){return carts.updateById(row);}
 public int remove(long id){return carts.deleteById(id);}
 public int removeForUser(long id,long user){return carts.delete(new LambdaQueryWrapper<Cart>().eq(Cart::getId,id).eq(Cart::getUserId,user));}
}`

func plusSourceFixture(method, call, sql string, names ...string) (Statement, map[string][]byte) {
	statement := Statement{ID: "plus", SQL: sql, SourcePath: "Repository.java", Source: &StatementSource{Strategy: "mybatis-plus", Namespace: "sample.CartMapper", StatementID: method, Plus: &PlusSource{EntityPath: "Cart.java", MapperPath: "CartMapper.java", CallMethod: call, Version: "3.5.17", BuildPath: "pom.xml", ConfigPath: "application-plus.properties"}}}
	for _, name := range names {
		statement.Parameters = append(statement.Parameters, Parameter{Name: name, Type: "BIGINT", Allowed: []mysqlv1.Value{mysqlv1.Int(1)}})
	}
	files := map[string][]byte{"Cart.java": []byte(plusCartJava), "CartMapper.java": []byte(plusMapperJava), "Repository.java": []byte(plusRepositoryJava), "pom.xml": []byte(`<project><dependencies><dependency><groupId>com.baomidou</groupId><artifactId>mybatis-plus-spring-boot3-starter</artifactId><version>3.5.17</version></dependency></dependencies></project>`), "application-plus.properties": []byte("mybatis-plus.configuration.map-underscore-to-camel-case=true\nmybatis-plus.configuration.cache-enabled=false\nmybatis-plus.configuration.local-cache-scope=STATEMENT\n")}
	return statement, files
}

func TestPlusSourceBaseMapperAndWrapper(t *testing.T) {
	for _, tc := range []struct {
		method, call, sql string
		names             []string
	}{
		{"selectById", "detail", "SELECT id,user_id,quantity FROM cart_items WHERE id=?", []string{"id"}},
		{"selectList", "list", "SELECT id,user_id,quantity FROM cart_items WHERE (user_id=?) ORDER BY id", []string{"ew.paramNameValuePairs.MPGENVAL1"}},
		{"selectOne", "item", "SELECT id,user_id,quantity FROM cart_items WHERE (user_id=? AND id=?)", []string{"ew.paramNameValuePairs.MPGENVAL1", "ew.paramNameValuePairs.MPGENVAL2"}},
		{"insert", "insert", "INSERT INTO cart_items (user_id,quantity) VALUES (?,?)", []string{"userId", "quantity"}},
		{"updateById", "update", "UPDATE cart_items SET user_id=?,quantity=? WHERE id=?", []string{"et.userId", "et.quantity", "et.id"}},
		{"deleteById", "remove", "DELETE FROM cart_items WHERE id=?", []string{"id"}},
		{"delete", "removeForUser", "DELETE FROM cart_items WHERE (id=? AND user_id=?)", []string{"ew.paramNameValuePairs.MPGENVAL1", "ew.paramNameValuePairs.MPGENVAL2"}},
	} {
		t.Run(tc.method, func(t *testing.T) {
			s, files := plusSourceFixture(tc.method, tc.call, tc.sql, tc.names...)
			if err := validateStatementSource(s, files); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestPlusSourceRejectsChangedEvidenceAndUnsupportedCalls(t *testing.T) {
	for _, name := range []string{"missing-entity", "wrong-table", "wrong-mapper", "wrong-namespace", "missing-condition", "swapped-conditions", "raw-sql", "conditional-eq", "or", "variable-wrapper", "wrong-param-name", "wrong-version", "pom-version", "config-override", "xml-override"} {
		t.Run(name, func(t *testing.T) {
			s, files := plusSourceFixture("selectOne", "item", "SELECT id,user_id,quantity FROM cart_items WHERE (user_id=? AND id=?)", "ew.paramNameValuePairs.MPGENVAL1", "ew.paramNameValuePairs.MPGENVAL2")
			change := func(path, from, to string) { files[path] = []byte(strings.ReplaceAll(string(files[path]), from, to)) }
			switch name {
			case "missing-entity":
				delete(files, "Cart.java")
			case "wrong-table":
				change("Cart.java", `@TableName("cart_items")`, `@TableName("other")`)
			case "wrong-mapper":
				change("Repository.java", "CartMapper carts", "OtherMapper carts")
			case "wrong-namespace":
				s.Source.Namespace = "other.CartMapper"
			case "missing-condition":
				change("Repository.java", ".eq(Cart::getUserId,user).eq(Cart::getId,id)", ".eq(Cart::getId,id)")
			case "swapped-conditions":
				change("Repository.java", ".eq(Cart::getUserId,user).eq(Cart::getId,id)", ".eq(Cart::getId,id).eq(Cart::getUserId,user)")
			case "raw-sql":
				change("Repository.java", ".eq(Cart::getUserId,user)", `.apply("user_id={0}",user)`)
			case "conditional-eq":
				change("Repository.java", ".eq(Cart::getUserId,user)", ".eq(user>0,Cart::getUserId,user)")
			case "or":
				change("Repository.java", ".eq(Cart::getUserId,user).eq(Cart::getId,id)", ".eq(Cart::getUserId,user).or().eq(Cart::getId,id)")
			case "variable-wrapper":
				change("Repository.java", "new LambdaQueryWrapper<Cart>().eq(Cart::getUserId,user).eq(Cart::getId,id)", "wrapper")
			case "wrong-param-name":
				s.Parameters[0].Name = "user"
			case "wrong-version":
				s.Source.Plus.Version = "3.5.99"
			case "pom-version":
				change("pom.xml", "3.5.17", "3.5.16")
			case "config-override":
				files["application-plus.properties"] = []byte("mybatis-plus.global-config.db-config.table-prefix=other_\n")
			case "xml-override":
				files["mapper.xml"] = []byte(`<mapper namespace="sample.CartMapper"><select id="selectList">SELECT * FROM other</select></mapper>`)
			}
			if err := validateStatementSource(s, files); err == nil {
				t.Fatal("unverified Plus SQL accepted")
			}
		})
	}
}
