package plugin

import (
	"encoding/json"
	"fmt"

	"github.com/kratejs/krate/packages/compiler/internal/astjson"
	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/jsruntime"
	pluginsdk "github.com/kratejs/krate/packages/compiler/pluginsdk"
)

// runGoPluginHook routes one lifecycle hook invocation to a Go plugin
// subprocess. The referenced Go hook contexts are built for the wire from the
// in-process typed context and their returned output is applied via the shared
// applyPluginOutput path.
func runGoPluginHook(hookName string, pc config.PluginConfig, root, outDir string, hookCtx interface{}) error {
	host, err := goPluginFor(pc)
	if err != nil {
		return err
	}

	args, err := buildGoHookArgs(hookName, root, outDir, hookCtx)
	if err != nil {
		return err
	}

	output, err := host.dispatch("build", hookName, args)
	if err != nil {
		return err
	}

	// Translate the Go-side Result into the JS communityOutput wiring so the
	// same host-side application logic (files, routes, HTML, head, scripts,
	// meta tags, and the AfterParse AST doc) runs for both plugin systems.
	trans := communityOutput{
		Routes:   translateRoutes(output.Routes),
		Scripts:  output.Scripts,
		MetaTags: output.MetaTags,
	}
	for _, f := range output.Files {
		trans.Files = append(trans.Files, fileEntry{Path: f.Path, Content: f.Content})
	}
	for _, gp := range output.GeneratedPages {
		trans.GeneratedPages = append(trans.GeneratedPages, GeneratedPage{Path: gp.Path, Route: gp.Route})
	}
	trans.HTML = output.HTML
	trans.HeadHTML = output.HeadHTML
	trans.RawCSS = output.RawCSS
	if len(output.Ast) > 0 {
		trans.Ast = output.Ast
	}

	return applyPluginOutput(hookName, &trans, outDir, hookCtx)
}

// translateRoutes converts Go-side Route values into the host Route type.
func translateRoutes(in []pluginsdk.Route) []Route {
	if len(in) == 0 {
		return nil
	}
	out := make([]Route, len(in))
	for i, r := range in {
		out[i] = Route{Path: r.Path, Content: r.Content, Title: r.Title, Layout: r.Layout, Data: r.Data}
	}
	return out
}

// buildGoHookArgs constructs the wire arguments for a hook. The AfterParse
// context carries the program as an astjson document, mirroring the JS path.
func buildGoHookArgs(hookName string, root, outDir string, hookCtx interface{}) (interface{}, error) {
	switch hookName {
	case "BeforeBuild", "GenerateRoutes", "AfterBuild":
		return hookCtx, nil
	case "AfterParse":
		pctx, ok := hookCtx.(*ParseHookCtx)
		if !ok || pctx == nil {
			return hookCtx, nil
		}
		doc := map[string]interface{}{"page": pctx.Page}
		if pctx.Program != nil {
			enc, err := astjson.EncodeProgram(pctx.Program)
			if err != nil {
				return nil, err
			}
			doc["program"] = json.RawMessage(enc)
		}
		return doc, nil
	default:
		return hookCtx, nil
	}
}

// runJSManifest bundles a plugin module (unless it is directly a Go binary)
// and runs its factory once, returning the static descriptor it exposes. The
// descriptor is how Krate discovers a Go plugin's runtime and per-platform
// binaries without a separate config surface.
func runJSManifest(module string) (goPluginDescriptor, error) {
	// A bare executable module is the binary itself; nothing to manifest.
	if bin, err := resolveGoBinaryFromModulePath(module); err == nil {
		return goPluginDescriptor{Runtime: "go", Binaries: map[string]string{platformKey(): bin}}, nil
	}

	bundleCode, err := bundleJSPlugin(module, ".")
	if err != nil {
		return goPluginDescriptor{}, err
	}
	rt, err := jsruntime.New()
	if err != nil {
		return goPluginDescriptor{}, fmt.Errorf("creating JS runtime for manifest: %w", err)
	}
	defer rt.Close()
	if _, err := rt.Execute(bundleCode); err != nil {
		return goPluginDescriptor{}, fmt.Errorf("loading plugin module for manifest: %w", err)
	}
	call := `(function(){
      try {
        var mod = __kratePlugin || {};
        var plugin = mod.default || mod;
        if (typeof plugin === 'function') plugin = plugin({});
        var desc = plugin || {};
        return JSON.stringify({ runtime: String(desc.runtime || ''), binaries: desc.binaries || {} });
      } catch (e) { return JSON.stringify({ error: (e && e.message) || String(e) }); }
    })()`
	res, err := rt.Execute(call)
	if err != nil {
		return goPluginDescriptor{}, fmt.Errorf("reading plugin manifest: %w", err)
	}
	var parsed struct {
		Runtime  string            `json:"runtime"`
		Binaries map[string]string `json:"binaries"`
		Error    string            `json:"error"`
	}
	if err := json.Unmarshal([]byte(res.(string)), &parsed); err != nil {
		return goPluginDescriptor{}, fmt.Errorf("decoding plugin manifest: %w", err)
	}
	if parsed.Error != "" {
		return goPluginDescriptor{}, fmt.Errorf("%s", parsed.Error)
	}
	return goPluginDescriptor{Runtime: parsed.Runtime, Binaries: parsed.Binaries}, nil
}
