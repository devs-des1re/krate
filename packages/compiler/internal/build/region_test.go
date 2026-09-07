package build

import (
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/irtree"
)

func TestEnumerateRegionsSuspensePrimary(t *testing.T) {
	primary := &irtree.ComponentNode{
		ID:         "page.suspense.0.Live",
		Name:       "Live",
		Tier:       irtree.TierRuntime,
		SourceFile: "src/Live.runtime.tsx",
	}
	tree := &irtree.ComponentTree{
		Root: &irtree.ComponentNode{
			Name: "Page",
			Tier: irtree.TierClient,
			Children: []irtree.SlotNode{
				&irtree.SuspenseSlot{
					ID:       "page.suspense.0",
					StreamID: "x-1",
					Mode:     irtree.SuspenseModeRegion,
					Primary:  primary,
				},
			},
		},
	}

	regions := enumerateRegions(tree)
	if len(regions) != 1 {
		t.Fatalf("expected 1 region, got %d: %+v", len(regions), regions)
	}
	r := regions[0]
	if r.ComponentName != "Live" {
		t.Errorf("expected component Live, got %q", r.ComponentName)
	}
	if r.SourcePath != "src/Live.runtime.tsx" {
		t.Errorf("expected sourcePath src/Live.runtime.tsx, got %q", r.SourcePath)
	}
	if r.ID != "x-1" {
		t.Errorf("expected region id from StreamID x-1, got %q", r.ID)
	}
	if !r.Suspense {
		t.Error("expected suspense flag for a suspense primary region")
	}
}

func TestEnumerateRegionsNestedSuspenseBoundary(t *testing.T) {
	// A runtime component nested inside a static wrapper within <Suspense> is
	// baked into the boundary's resolved content and enumerated as its own
	// standalone region (the boundary is never deferred as an anonymous whole,
	// since it has no component/bundle identity to render).
	tree := &irtree.ComponentTree{
		Root: &irtree.ComponentNode{
			Name: "Page",
			Tier: irtree.TierClient,
			Children: []irtree.SlotNode{
				&irtree.SuspenseSlot{
					ID:       "page.suspense.0",
					StreamID: "y-2",
					Mode:     irtree.SuspenseModeStatic,
					Resolved: []irtree.SlotNode{
						&irtree.ComponentSlot{
							ID: "page.suspense.0.div.Live",
							Component: &irtree.ComponentNode{
								ID:         "page.suspense.0.div.Live_c0",
								Name:       "Live",
								Tier:       irtree.TierRuntime,
								SourceFile: "src/Live.runtime.tsx",
							},
						},
					},
				},
			},
		},
	}

	regions := enumerateRegions(tree)
	if len(regions) != 1 {
		t.Fatalf("expected 1 region, got %d", len(regions))
	}
	if regions[0].ComponentName != "Live" {
		t.Errorf("expected component Live for the nested runtime region, got %q", regions[0].ComponentName)
	}
	if regions[0].ID != "region-page.suspense.0.div.Live" {
		t.Errorf("expected standalone region id, got %q", regions[0].ID)
	}
}

func TestEnumerateRegionsStandaloneRuntimeComponent(t *testing.T) {
	tree := &irtree.ComponentTree{
		Root: &irtree.ComponentNode{
			Name: "Page",
			Tier: irtree.TierClient,
			Children: []irtree.SlotNode{
				&irtree.ComponentSlot{
					ID: "page.live.0",
					Component: &irtree.ComponentNode{
						ID:         "page.live.0_c0",
						Name:       "Live",
						Tier:       irtree.TierRuntime,
						SourceFile: "src/Live.runtime.tsx",
					},
				},
				&irtree.ComponentSlot{
					ID: "page.static.0",
					Component: &irtree.ComponentNode{
						Name: "Static",
						Tier: irtree.TierClient,
					},
				},
			},
		},
	}

	regions := enumerateRegions(tree)
	if len(regions) != 1 {
		t.Fatalf("expected 1 region (only runtime), got %d: %+v", len(regions), regions)
	}
	r := regions[0]
	if r.ComponentName != "Live" {
		t.Errorf("expected component Live, got %q", r.ComponentName)
	}
	if r.ID != "region-page.live.0" {
		t.Errorf("expected id region-page.live.0, got %q", r.ID)
	}
	if r.Suspense {
		t.Error("expected suspense=false for a standalone runtime component")
	}
}

func TestEnumerateRegionsNonRegionSuspenseRecurses(t *testing.T) {
	// A static suspense boundary with a nested runtime component must still
	// surface that runtime component as a standalone region.
	innerRuntime := &irtree.ComponentNode{
		Name:       "Live",
		Tier:       irtree.TierRuntime,
		SourceFile: "src/Live.runtime.tsx",
	}
	tree := &irtree.ComponentTree{
		Root: &irtree.ComponentNode{
			Name: "Page",
			Tier: irtree.TierClient,
			Children: []irtree.SlotNode{
				&irtree.SuspenseSlot{
					ID:       "page.suspense.0",
					StreamID: "z-3",
					Mode:     irtree.SuspenseModeStatic,
					Resolved: []irtree.SlotNode{
						&irtree.ComponentSlot{
							ID:        "page.suspense.0.resolved.0",
							Component: innerRuntime,
						},
					},
				},
			},
		},
	}

	regions := enumerateRegions(tree)
	if len(regions) != 1 {
		t.Fatalf("expected 1 region from recursive walk, got %d", len(regions))
	}
	if regions[0].ComponentName != "Live" {
		t.Errorf("expected component Live from recursive walk, got %q", regions[0].ComponentName)
	}
}
