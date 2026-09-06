// Package main is a fixture Krate Go plugin used by the host e2e test. It
// exercises the BeforeBuild, AfterParse and AfterRender hooks built purely on
// the public SDK surface (github.com/kratejs/krate/packages/compiler/pluginsdk + github.com/kratejs/krate/packages/compiler/ast),
// matching what an external plugin author would write.
package main

import (
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/pluginsdk"
)

// editFirstJSXText replaces a token in the first text child of the returned
// JSX. The fixture page renders <b>go-plugin-token</b>.
func editFirstJSXText(prog *ast.Program) {
	exp, ok := prog.Body[0].(*ast.ExportStmt)
	if !ok {
		return
	}
	fn, ok := exp.Declaration.(*ast.FnDecl)
	if !ok {
		return
	}
	if len(fn.Body) == 0 {
		return
	}
	ret, ok := fn.Body[0].(*ast.ReturnStmt)
	if !ok {
		return
	}
	jsx, ok := ret.Value.(*ast.JSXElement)
	if !ok {
		return
	}
	if len(jsx.Children) == 0 {
		return
	}
	if text, ok := jsx.Children[0].(*ast.JSXText); ok {
		text.Value = strings.ReplaceAll(text.Value, "go-plugin-token", "GO-PLUGIN-EDITED")
	}
}

func main() {
	plug.Serve("fixture", plug.Hooks{
		BeforeBuild: func(ctx *plug.BuildArgs) error {
			ctx.Files = append(ctx.Files, plug.File{
				Path:    "fixture.txt",
				Content: "go plugin running from " + ctx.Root,
			})
			return nil
		},
		AfterParse: func(ctx *plug.ParseArgs) error {
			editFirstJSXText(ctx.Program)
			return nil
		},
		AfterRender: func(ctx *plug.RenderArgs) error {
			ctx.HeadHTML = ctx.HeadHTML + "\n<meta name=\"validator\" content=\"gofix\">"
			ctx.MetaTags = append(ctx.MetaTags, `name="fixture" content="true"`)
			return nil
		},
		AfterBuild: func(ctx *plug.BuildResultArgs) error {
			ctx.Files = append(ctx.Files, plug.File{
				Path:    "build-summary.txt",
				Content: "pages:" + itoa(len(ctx.Pages)),
			})
			return nil
		},
		ServeRequest: func(ctx *plug.ServeRequestArgs) error {
			if ctx.Path == "/__krate/veto" {
				ctx.Action = "respond"
				ctx.Status = 418
				ctx.Body = "teapot-told-me"
				ctx.Headers = map[string]string{"x-plugin": "go-serve"}
			}
			return nil
		},
		ServeResponse: func(ctx *plug.ServeResponseArgs) error {
			ctx.Body = ctx.Body + "\n<!-- served-by-go-plugin -->"
			if ctx.Headers == nil {
				ctx.Headers = map[string]string{}
			}
			ctx.Headers["x-go-plugin"] = "1"
			return nil
		},
	})
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	return "1"
}
