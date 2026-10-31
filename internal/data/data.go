// Package data は内蔵食品データベースと料理テンプレートのJSONを埋め込む。
// 元データは文部科学省「日本食品標準成分表」の代表値をもとにした概数（100gあたり）。
package data

import _ "embed"

//go:embed foods.json
var FoodsJSON []byte

//go:embed templates.json
var TemplatesJSON []byte
