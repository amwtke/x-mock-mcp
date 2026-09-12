package main

import (
	"fmt"
	sitter "github.com/tree-sitter/go-tree-sitter"
	"strings"
)

type plusCall struct {
	Predicates []plusField
	Order      []Order
}

func parsePlusCall(raw []byte, source StatementSource, entity plusEntity) (plusCall, error) {
	var result plusCall
	d, err := parsePlusJava(raw)
	if err != nil {
		return result, err
	}
	defer d.close()
	if d.decl.Kind() != "class_declaration" || javaChild(d.decl, "superclass") != nil {
		return result, fmt.Errorf("plain Mapper caller class required")
	}
	fields := map[string]bool{}
	var method *sitter.Node
	for _, member := range javaChildren(d.decl.ChildByFieldName("body")) {
		if member.Kind() == "field_declaration" && d.resolve(d.text(member.ChildByFieldName("type"))) == source.Namespace {
			for _, v := range javaChildren(member) {
				if v.Kind() == "variable_declarator" {
					fields[d.text(v.ChildByFieldName("name"))] = true
				}
			}
		}
		if member.Kind() == "method_declaration" && d.text(member.ChildByFieldName("name")) == source.Plus.CallMethod {
			if method != nil {
				return result, fmt.Errorf("overloaded call_method unsupported")
			}
			method = member
		}
	}
	if method == nil {
		return result, fmt.Errorf("call_method not found")
	}
	var selected *sitter.Node
	count := 0
	javaWalk(method.ChildByFieldName("body"), func(n *sitter.Node) {
		if n.Kind() != "method_invocation" || d.text(n.ChildByFieldName("name")) != source.StatementID {
			return
		}
		receiver := d.text(n.ChildByFieldName("object"))
		if fields[strings.TrimPrefix(receiver, "this.")] {
			selected = n
			count++
		}
	})
	if count != 1 {
		return result, fmt.Errorf("one grounded %s Mapper call required, found %d", source.StatementID, count)
	}
	for parent := selected.Parent(); parent != nil && parent.Id() != method.Id(); parent = parent.Parent() {
		switch parent.Kind() {
		case "if_statement", "for_statement", "enhanced_for_statement", "while_statement", "do_statement", "switch_expression", "lambda_expression", "class_body":
			return result, fmt.Errorf("conditional/nested Mapper call unsupported")
		}
	}
	receiver := d.text(selected.ChildByFieldName("object"))
	if !strings.HasPrefix(receiver, "this.") {
		shadow := false
		javaWalk(method, func(n *sitter.Node) {
			if (n.Kind() == "formal_parameter" || n.Kind() == "variable_declarator") && d.text(n.ChildByFieldName("name")) == receiver {
				shadow = true
			}
		})
		if shadow {
			return result, fmt.Errorf("Mapper receiver is shadowed by a local binding")
		}
	}
	args := javaChildren(selected.ChildByFieldName("arguments"))
	if len(args) != 1 {
		return result, fmt.Errorf("only one-argument BaseMapper overload supported")
	}
	switch source.StatementID {
	case "selectById", "deleteById":
		return result, nil
	case "insert", "updateById":
		if args[0].Kind() != "identifier" {
			return result, fmt.Errorf("named full entity argument required")
		}
		name := d.text(args[0])
		types := []string{}
		javaWalk(method, func(n *sitter.Node) {
			if n.Kind() == "formal_parameter" && d.text(n.ChildByFieldName("name")) == name {
				types = append(types, d.resolve(d.text(n.ChildByFieldName("type"))))
			}
			if n.Kind() == "local_variable_declaration" {
				for _, v := range javaChildren(n) {
					if v.Kind() == "variable_declarator" && d.text(v.ChildByFieldName("name")) == name {
						types = append(types, d.resolve(d.text(n.ChildByFieldName("type"))))
					}
				}
			}
		})
		if len(types) != 1 || types[0] != entity.Name {
			return result, fmt.Errorf("CRUD argument must be the declared entity type")
		}
		return result, nil
	case "selectList", "selectOne", "delete":
	default:
		return result, fmt.Errorf("unsupported BaseMapper method %s", source.StatementID)
	}
	var chain []*sitter.Node
	node := args[0]
	for node != nil && node.Kind() == "method_invocation" {
		chain = append(chain, node)
		node = node.ChildByFieldName("object")
	}
	if node == nil || node.Kind() != "object_creation_expression" || len(javaChildren(node.ChildByFieldName("arguments"))) != 0 || javaChild(node, "class_body") != nil {
		return result, fmt.Errorf("direct new LambdaQueryWrapper<Entity>() required")
	}
	typ := node.ChildByFieldName("type")
	parts := javaChildren(typ)
	if typ.Kind() != "generic_type" || len(parts) != 2 || d.resolve(d.text(parts[0])) != "com.baomidou.mybatisplus.core.conditions.query.LambdaQueryWrapper" {
		return result, fmt.Errorf("actual LambdaQueryWrapper type required")
	}
	typeArgs := javaChildren(parts[1])
	if len(typeArgs) != 1 || d.resolve(d.text(typeArgs[0])) != entity.Name {
		return result, fmt.Errorf("Wrapper entity mismatch")
	}
	for i := len(chain) - 1; i >= 0; i-- {
		node := chain[i]
		name := d.text(node.ChildByFieldName("name"))
		args := javaChildren(node.ChildByFieldName("arguments"))
		if (name == "eq" && len(args) != 2) || ((name == "orderByAsc" || name == "orderByDesc") && len(args) != 1) {
			return result, fmt.Errorf("conditional/variadic Wrapper overload unsupported")
		}
		if name != "eq" && name != "orderByAsc" && name != "orderByDesc" {
			return result, fmt.Errorf("unsupported Wrapper method %s", name)
		}
		if args[0].Kind() != "method_reference" {
			return result, fmt.Errorf("entity getter method reference required")
		}
		typ, getter, ok := strings.Cut(d.text(args[0]), "::")
		if !ok || d.resolve(strings.TrimSpace(typ)) != entity.Name {
			return result, fmt.Errorf("Wrapper getter entity mismatch")
		}
		var field plusField
		found := false
		for _, f := range entity.Fields {
			if strings.TrimSpace(getter) == "get"+strings.ToUpper(f.Property[:1])+f.Property[1:] {
				field = f
				found = true
			}
		}
		if !found {
			return result, fmt.Errorf("unknown Wrapper getter")
		}
		if name == "eq" {
			result.Predicates = append(result.Predicates, field)
		} else {
			result.Order = append(result.Order, Order{Column: field.Column, Descending: name == "orderByDesc"})
		}
	}
	if source.StatementID == "delete" && (len(result.Predicates) == 0 || len(result.Order) != 0) {
		return result, fmt.Errorf("DELETE requires predicates and no ordering")
	}
	return result, nil
}
