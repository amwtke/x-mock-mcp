package main

import (
	"fmt"
	"strings"
)

const plusAnnotations = "com.baomidou.mybatisplus.annotation."

type plusField struct {
	Property, Column, Type string
	Primary, Auto          bool
}
type plusEntity struct {
	Name, Table string
	Fields      []plusField
}

func (e plusEntity) field(property string) (plusField, bool) {
	for _, f := range e.Fields {
		if f.Property == property {
			return f, true
		}
	}
	return plusField{}, false
}
func (e plusEntity) key() plusField {
	for _, f := range e.Fields {
		if f.Primary {
			return f
		}
	}
	return plusField{}
}

func parsePlusEntity(raw []byte) (plusEntity, error) {
	var result plusEntity
	d, err := parsePlusJava(raw)
	if err != nil {
		return result, err
	}
	defer d.close()
	if d.decl.Kind() != "class_declaration" || javaChild(d.decl, "superclass") != nil || javaChild(d.decl, "super_interfaces") != nil || d.decl.ChildByFieldName("type_parameters") != nil {
		return result, fmt.Errorf("plain non-inherited Plus entity required")
	}
	annotations, err := d.annotations(d.decl)
	if err != nil {
		return result, err
	}
	table, ok := annotations[plusAnnotations+"TableName"]
	if !ok || len(annotations) != 1 || len(table) != 1 {
		return result, fmt.Errorf("explicit plain @TableName required")
	}
	result.Table, err = d.literal(table["value"])
	if err != nil || !simpleSQLName(result.Table) {
		return result, fmt.Errorf("literal simple @TableName required")
	}
	result.Name = d.name()
	columns := map[string]bool{}
	keys := 0
	for _, member := range javaChildren(d.decl.ChildByFieldName("body")) {
		switch member.Kind() {
		case "field_declaration":
			if modifiers := javaChild(member, "modifiers"); modifiers != nil {
				for i := uint(0); i < modifiers.ChildCount(); i++ {
					kind := modifiers.Child(i).Kind()
					if kind == "static" || kind == "transient" {
						return result, fmt.Errorf("static/transient entity properties unsupported")
					}
				}
			}
			vars := javaChildren(member)
			var field plusField
			count := 0
			for _, v := range vars {
				if v.Kind() == "variable_declarator" {
					if v.ChildByFieldName("dimensions") != nil {
						return result, fmt.Errorf("array entity properties unsupported")
					}
					count++
					field.Property = d.text(v.ChildByFieldName("name"))
					if v.ChildByFieldName("value") != nil {
						return result, fmt.Errorf("entity field initializers unsupported")
					}
				}
			}
			if count != 1 {
				return result, fmt.Errorf("one property per field required")
			}
			switch d.resolve(d.text(member.ChildByFieldName("type"))) {
			case "java.lang.Long":
				field.Type = "BIGINT"
			case "java.lang.String":
				field.Type = "VARCHAR"
			default:
				return result, fmt.Errorf("only Long/String entity properties supported")
			}
			attrs, err := d.annotations(member)
			if err != nil {
				return result, err
			}
			if len(attrs) != 1 {
				return result, fmt.Errorf("one explicit TableId/TableField mapping required; field extensions unsupported")
			}
			for name, values := range attrs {
				switch name {
				case plusAnnotations + "TableId":
					if len(values) != 2 {
						return result, fmt.Errorf("TableId requires literal value and AUTO/INPUT type")
					}
					node := values["type"]
					text := d.text(node)
					parts := strings.Split(text, ".")
					if len(parts) < 2 || d.resolve(strings.Join(parts[:len(parts)-1], ".")) != plusAnnotations+"IdType" || (parts[len(parts)-1] != "AUTO" && parts[len(parts)-1] != "INPUT") {
						return result, fmt.Errorf("only actual IdType.AUTO/INPUT supported")
					}
					field.Primary = true
					field.Auto = parts[len(parts)-1] == "AUTO"
					keys++
					if field.Type != "BIGINT" {
						return result, fmt.Errorf("BIGINT primary key required")
					}
				case plusAnnotations + "TableField":
					if len(values) != 1 {
						return result, fmt.Errorf("TableField extensions unsupported")
					}
				default:
					return result, fmt.Errorf("unsupported entity annotation %s", name)
				}
				field.Column, err = d.literal(values["value"])
				if err != nil {
					return result, err
				}
			}
			if !simpleSQLName(field.Column) || !simpleSQLName(field.Property) || columns[field.Column] {
				return result, fmt.Errorf("invalid/duplicate entity column")
			}
			columns[field.Column] = true
			result.Fields = append(result.Fields, field)
		case "method_declaration", "constructor_declaration":
		default:
			return result, fmt.Errorf("unsupported entity member %s", member.Kind())
		}
	}
	if keys != 1 || len(result.Fields) < 1 {
		return result, fmt.Errorf("exactly one explicit TableId required")
	}
	for _, field := range result.Fields {
		getter := "get" + strings.ToUpper(field.Property[:1]) + field.Property[1:]
		found := false
		for _, method := range javaChildren(d.decl.ChildByFieldName("body")) {
			if method.Kind() != "method_declaration" || d.text(method.ChildByFieldName("name")) != getter {
				continue
			}
			if found {
				return result, fmt.Errorf("overloaded getter unsupported")
			}
			found = true
			body := javaChildren(method.ChildByFieldName("body"))
			if len(javaChildren(method.ChildByFieldName("parameters"))) != 0 || len(body) != 1 || body[0].Kind() != "return_statement" {
				return result, fmt.Errorf("simple getter required for %s", field.Property)
			}
			value := javaChildren(body[0])
			if len(value) != 1 || (d.text(value[0]) != field.Property && d.text(value[0]) != "this."+field.Property) {
				return result, fmt.Errorf("computed getter unsupported for %s", field.Property)
			}
		}
		if !found {
			return result, fmt.Errorf("getter required for %s", field.Property)
		}
	}
	return result, nil
}

func simpleSQLName(name string) bool {
	return mapperProperty.MatchString(name) && !strings.Contains(name, ".") && !strings.Contains(name, "$")
}

func validatePlusMapper(raw []byte, namespace, entity string) error {
	d, err := parsePlusJava(raw)
	if err != nil {
		return err
	}
	defer d.close()
	if d.name() != namespace || d.decl.Kind() != "interface_declaration" {
		return fmt.Errorf("Mapper namespace/interface mismatch")
	}
	if len(javaChildren(d.decl.ChildByFieldName("body"))) != 0 {
		return fmt.Errorf("BaseMapper method overrides unsupported")
	}
	extension := javaChild(d.decl, "extends_interfaces")
	list := javaChildren(extension)
	if len(list) == 1 && list[0].Kind() == "type_list" {
		list = javaChildren(list[0])
	}
	if len(list) != 1 || list[0].Kind() != "generic_type" {
		return fmt.Errorf("direct BaseMapper<Entity> inheritance required")
	}
	parts := javaChildren(list[0])
	if len(parts) != 2 || d.resolve(d.text(parts[0])) != "com.baomidou.mybatisplus.core.mapper.BaseMapper" {
		return fmt.Errorf("actual BaseMapper import required")
	}
	arguments := javaChildren(parts[1])
	if len(arguments) != 1 || d.resolve(d.text(arguments[0])) != entity {
		return fmt.Errorf("BaseMapper entity mismatch")
	}
	return nil
}
