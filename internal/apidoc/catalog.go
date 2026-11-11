// Package apidoc serves the reviewed API contract alongside registered routes.
package apidoc

import (
	_ "embed"
	"encoding/json"
	"fmt"

	"gin-biz-web-api/model"
)

//go:embed catalog.json
var catalogJSON []byte

type Field struct {
	Name        string  `json:"name"`
	Type        string  `json:"type"`
	Required    bool    `json:"required"`
	Description string  `json:"description"`
	Nullable    bool    `json:"nullable,omitempty"`
	Location    string  `json:"location,omitempty"`
	Example     any     `json:"example,omitempty"`
	Children    []Field `json:"children,omitempty"`
}

type Note struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

type Error struct {
	Status      int    `json:"status"`
	Code        string `json:"code"`
	Description string `json:"description"`
}

type Operation struct {
	ID               string          `json:"id"`
	Method           string          `json:"method"`
	Path             string          `json:"path"`
	Title            string          `json:"title"`
	Description      string          `json:"description"`
	Permission       string          `json:"permission"`
	ParentPermission string          `json:"parentPermission"`
	Authentication   string          `json:"authentication"`
	RequestFields    []Field         `json:"requestFields"`
	ResponseFields   []Field         `json:"responseFields"`
	RequestExample   json.RawMessage `json:"requestExample"`
	ResponseExample  json.RawMessage `json:"responseExample"`
	Errors           []Error         `json:"errors"`
	Notes            []Note          `json:"notes"`
}

type Category struct {
	Code       string      `json:"code"`
	Name       string      `json:"name"`
	Operations []Operation `json:"operations"`
}

type Group struct {
	Code        string     `json:"code"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Children    []Category `json:"children"`
}

type Document struct {
	Title       string  `json:"title"`
	Version     string  `json:"version"`
	Description string  `json:"description"`
	Groups      []Group `json:"groups"`
}

// Build excludes unavailable routes and uses the same permission hierarchy as
// authorization. The catalog is embedded so reading docs needs no database.
func Build(registered map[string]bool) (Document, error) {
	var source struct {
		Title      string      `json:"title"`
		Version    string      `json:"version"`
		Operations []Operation `json:"operations"`
	}
	if err := json.Unmarshal(catalogJSON, &source); err != nil {
		return Document{}, fmt.Errorf("decode API catalog: %w", err)
	}
	document := Document{
		Title: source.Title, Version: source.Version,
		Description: "开放接口的请求、响应和字段规则。目录随服务实际路由发布，一级权限覆盖所属二级接口。",
		Groups:      []Group{},
	}
	for _, definition := range model.OpenAPIPermissionGroups() {
		group := Group{Code: definition.Code, Name: definition.Name, Description: definition.Description, Children: []Category{}}
		for _, permission := range definition.Children {
			category := Category{Code: permission.Code, Name: permission.Name, Operations: []Operation{}}
			for _, operation := range source.Operations {
				if operation.Permission != permission.Code || !registered[operation.Method+" "+operation.Path] {
					continue
				}
				operation.ParentPermission = definition.Code
				category.Operations = append(category.Operations, operation)
			}
			if len(category.Operations) > 0 {
				group.Children = append(group.Children, category)
			}
		}
		if len(group.Children) > 0 {
			document.Groups = append(document.Groups, group)
		}
	}
	return document, nil
}
