package main

import (
	"fmt"
	"strconv"
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"
	java "github.com/tree-sitter/tree-sitter-java/bindings/go"
)

type plusJava struct {
	raw     []byte
	tree    *sitter.Tree
	decl    *sitter.Node
	pkg     string
	imports map[string]string
}

func parsePlusJava(raw []byte) (*plusJava, error) {
	if len(raw) > 1<<20 || strings.Contains(string(raw), `\u`) {
		return nil, fmt.Errorf("Java source too large or Unicode escapes unsupported")
	}
	parser := sitter.NewParser()
	defer parser.Close()
	if err := parser.SetLanguage(sitter.NewLanguage(java.Language())); err != nil {
		return nil, err
	}
	parser.SetTimeoutMicros(500000)
	tree := parser.Parse(raw, nil)
	if tree == nil {
		return nil, fmt.Errorf("Java parse deadline exceeded")
	}
	d := &plusJava{raw: raw, tree: tree, imports: map[string]string{}}
	fail := func(message string) (*plusJava, error) {
		tree.Close()
		return nil, fmt.Errorf("Java source: %s", message)
	}
	root := tree.RootNode()
	if root.HasError() {
		return fail("invalid or unsupported syntax")
	}
	for _, node := range javaChildren(root) {
		text := d.text(node)
		switch node.Kind() {
		case "package_declaration":
			d.pkg = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(text, "package"), ";"))
			if !mapperProperty.MatchString(d.pkg) {
				return fail("simple package declaration required")
			}
		case "import_declaration":
			name := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(text, "import"), ";"))
			if strings.HasPrefix(name, "static ") {
				return fail("static imports unsupported in Plus evidence")
			}
			if strings.HasSuffix(name, ".*") {
				if name != "com.baomidou.mybatisplus.annotation.*" {
					return fail("use explicit imports in Plus evidence")
				}
				for _, short := range []string{"TableName", "TableField", "TableId", "IdType", "TableLogic", "Version", "FieldFill"} {
					d.imports[short] = "com.baomidou.mybatisplus.annotation." + short
				}
			} else {
				parts := strings.Split(name, ".")
				short := parts[len(parts)-1]
				if _, exists := d.imports[short]; exists {
					return fail("ambiguous imports")
				}
				d.imports[short] = name
			}
		case "class_declaration", "interface_declaration":
			if d.decl != nil {
				return fail("one top-level declaration required")
			}
			d.decl = node
		case "line_comment", "block_comment":
		default:
			return fail("unsupported top-level declaration " + node.Kind())
		}
	}
	if d.pkg == "" || d.decl == nil {
		return fail("package and top-level declaration required")
	}
	return d, nil
}
func (d *plusJava) close() { d.tree.Close() }
func (d *plusJava) text(n *sitter.Node) string {
	if n == nil {
		return ""
	}
	return n.Utf8Text(d.raw)
}
func (d *plusJava) name() string { return d.pkg + "." + d.text(d.decl.ChildByFieldName("name")) }
func (d *plusJava) resolve(name string) string {
	if strings.Contains(name, ".") {
		return name
	}
	if full, ok := d.imports[name]; ok {
		return full
	}
	if name == "String" || name == "Long" {
		return "java.lang." + name
	}
	return d.pkg + "." + name
}
func javaChildren(n *sitter.Node) []*sitter.Node {
	var out []*sitter.Node
	if n != nil {
		for i := uint(0); i < n.NamedChildCount(); i++ {
			child := n.NamedChild(i)
			if child.Kind() != "line_comment" && child.Kind() != "block_comment" {
				out = append(out, child)
			}
		}
	}
	return out
}
func javaChild(n *sitter.Node, kind string) *sitter.Node {
	for _, child := range javaChildren(n) {
		if child.Kind() == kind {
			return child
		}
	}
	return nil
}
func javaWalk(n *sitter.Node, visit func(*sitter.Node)) {
	stack := []*sitter.Node{n}
	for len(stack) > 0 {
		last := len(stack) - 1
		node := stack[last]
		stack = stack[:last]
		if node == nil {
			continue
		}
		visit(node)
		stack = append(stack, javaChildren(node)...)
	}
}
func (d *plusJava) annotations(n *sitter.Node) (map[string]map[string]*sitter.Node, error) {
	result := map[string]map[string]*sitter.Node{}
	for _, node := range javaChildren(javaChild(n, "modifiers")) {
		if node.Kind() != "annotation" && node.Kind() != "marker_annotation" {
			continue
		}
		name := d.resolve(d.text(node.ChildByFieldName("name")))
		if _, exists := result[name]; exists {
			return nil, fmt.Errorf("duplicate annotation %s", name)
		}
		values := map[string]*sitter.Node{}
		for _, arg := range javaChildren(node.ChildByFieldName("arguments")) {
			key, value := "value", arg
			if arg.Kind() == "element_value_pair" {
				key = d.text(arg.ChildByFieldName("key"))
				value = arg.ChildByFieldName("value")
			}
			if _, exists := values[key]; exists {
				return nil, fmt.Errorf("duplicate annotation property %s", key)
			}
			values[key] = value
		}
		result[name] = values
	}
	return result, nil
}
func (d *plusJava) literal(n *sitter.Node) (string, error) {
	if n == nil || n.Kind() != "string_literal" {
		return "", fmt.Errorf("literal Java string required")
	}
	value, err := strconv.Unquote(d.text(n))
	if err != nil {
		return "", fmt.Errorf("simple Java string required: %w", err)
	}
	return value, nil
}
