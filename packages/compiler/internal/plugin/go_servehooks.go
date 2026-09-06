package plugin

import (
	"encoding/json"
	"fmt"

	"github.com/kratejs/krate/packages/compiler/internal/config"
	pluginsdk "github.com/kratejs/krate/packages/compiler/pluginsdk"
)

// runGoServeRequest dispatches the ServeRequest hook to a Go plugin subprocess.
func runGoServeRequest(pc config.PluginConfig, root string, args ServeRequestArgs) (*ServeRequestResult, error) {
	host, err := goPluginFor(pc)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(args)
	if err != nil {
		return nil, err
	}
	out, err := host.rawDispatch("serve", "ServeRequest", raw)
	if err != nil {
		return nil, err
	}
	var ctx pluginsdk.ServeRequestArgs
	if err := json.Unmarshal(out, &ctx); err != nil {
		return nil, fmt.Errorf("decoding ServeRequest result: %w", err)
	}
	res := &ServeRequestResult{
		Action:  ctx.Action,
		Status:  ctx.Status,
		NewURL:  ctx.NewURL,
		Body:    ctx.Body,
		Headers: ctx.Headers,
	}
	return res, nil
}

// runGoServeResponse dispatches the ServeResponse hook to a Go plugin subprocess.
func runGoServeResponse(pc config.PluginConfig, root string, args ServeResponseArgs) (ServeResponseResult, error) {
	host, err := goPluginFor(pc)
	if err != nil {
		return ServeResponseResult{}, err
	}
	raw, err := json.Marshal(args)
	if err != nil {
		return ServeResponseResult{}, err
	}
	out, err := host.rawDispatch("serve", "ServeResponse", raw)
	if err != nil {
		return ServeResponseResult{}, err
	}
	var ctx pluginsdk.ServeResponseArgs
	if err := json.Unmarshal(out, &ctx); err != nil {
		return ServeResponseResult{}, fmt.Errorf("decoding ServeResponse result: %w", err)
	}
	return ServeResponseResult{
		Status:  ctx.Status,
		Headers: ctx.Headers,
		Body:    ctx.Body,
	}, nil
}
