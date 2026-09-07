package build

import (
	"github.com/kratejs/krate/packages/compiler/internal/irtree"
)

// Region describes a single dynamic region within a server-rendered page. Each
// region is rendered at request time by the SSR sidecar and spliced into the
// static shell at its marker by the Go server.
type Region struct {
	// ID is a stable, deterministic splice-marker key (matches the shell).
	ID string `json:"id"`
	// ComponentName is the component rendered by this region ("" for boundary
	// regions that re-render a whole Suspense boundary).
	ComponentName string `json:"component,omitempty"`
	// SourcePath is the source file defining the component (relative to root).
	SourcePath string `json:"sourcePath,omitempty"`
	// Props are the build-time resolved props for the region's component. They
	// were evaluated at compile time and baked into the region registry, so
	// region rendering at request time never re-derives them (static-first).
	Props map[string]any `json:"props,omitempty"`
	// Suspense indicates the region is a Suspense primary/boundary (streamed).
	Suspense bool `json:"suspense,omitempty"`
}

// RegionMeta is the serializable subset of Region written to the manifests.
type RegionMeta struct {
	ID         string         `json:"id"`
	Component  string         `json:"component,omitempty"`
	SourcePath string         `json:"sourcePath,omitempty"`
	BundlePath string         `json:"bundlePath,omitempty"`
	Props      map[string]any `json:"props,omitempty"`
	Suspense   bool           `json:"suspense,omitempty"`
}

// enumerateRegions walks a page's IR tree and collects every dynamic region in
// deterministic (tree) order:
//
//   - A ModeRegion SuspenseSlot → one region for the boundary. If it has a
//     top-level runtime primary, the primary identity is captured; otherwise the
//     whole boundary is re-rendered (ComponentName empty, keyed by StreamID).
//   - Standalone TierRuntime ComponentNode → one region per instance.
func enumerateRegions(tree *irtree.ComponentTree) []Region {
	if tree == nil || tree.Root == nil {
		return nil
	}
	var regions []Region
	walkRegions(tree.Root, func(r Region) {
		regions = append(regions, r)
	})
	return regions
}

// walkRegions recursively descends a component node's slot children looking for
// dynamic regions, invoking emit for each found region in tree order.
func walkRegions(node *irtree.ComponentNode, emit func(Region)) {
	if node == nil {
		return
	}
	for _, child := range node.Children {
		walkSlotRegions(child, emit)
	}
}

func walkSlotRegions(slot irtree.SlotNode, emit func(Region)) {
	switch s := slot.(type) {
	case *irtree.ComponentSlot:
		if s.Component != nil && s.Component.Tier == irtree.TierRuntime {
			// Standalone runtime component — one region per instance.
			emit(Region{
				ID:            "region-" + string(s.ID),
				ComponentName: s.Component.Name,
				SourcePath:    s.Component.SourceFile,
				Props:         s.Component.RuntimeProps,
			})
			return
		}
		walkRegions(s.Component, emit)
	case *irtree.SuspenseSlot:
		if s.Mode == irtree.SuspenseModeRegion {
			id := s.StreamID
			if id == "" {
				id = "region-" + string(s.ID)
			}
			if s.Primary != nil {
				emit(Region{
					ID:            id,
					ComponentName: s.Primary.Name,
					SourcePath:    s.Primary.SourceFile,
					Props:         s.Primary.RuntimeProps,
					Suspense:      true,
				})
			} else {
				emit(Region{
					ID:       id,
					Suspense: true,
				})
			}
			// The boundary renders as one unit — do not descend into its
			// baked content (standalone nested regions surface separately when
			// the boundary itself is re-rendered).
			return
		}
		// Non-region modes: recurse through baked content so any nested dynamic
		// regions (runtime components) still surface as standalone regions.
		for _, fb := range s.Fallback {
			walkSlotRegions(fb, emit)
		}
		for _, rs := range s.Resolved {
			walkSlotRegions(rs, emit)
		}
		if s.Primary != nil {
			walkRegions(s.Primary, emit)
		}
	case *irtree.ConditionalSlot:
		for _, c := range s.Consequent {
			walkSlotRegions(c, emit)
		}
		for _, c := range s.Alternate {
			walkSlotRegions(c, emit)
		}
	case *irtree.ListSlot:
		for _, item := range s.Items {
			for _, c := range item.Contents {
				walkSlotRegions(c, emit)
			}
		}
	}
}
