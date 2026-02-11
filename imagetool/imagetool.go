package imagetool

import (
	"context"
	"sync"

	"github.com/flexigpt/llmtools-go/internal/fileutil"
	"github.com/flexigpt/llmtools-go/internal/toolutil"
	"github.com/flexigpt/llmtools-go/spec"
)

// imageToolPolicy centralizes sandbox/path policy for this tool instance.
type imageToolPolicy struct {
	allowedRoots  []string
	workBaseDir   string
	blockSymlinks bool
}

// Clone returns an independent copy of the policy.
func (p *imageToolPolicy) Clone() *imageToolPolicy {
	if p == nil {
		return nil
	}
	cp := *p
	cp.allowedRoots = append([]string(nil), p.allowedRoots...)
	return &cp
}

// ImageTool is an instance-owned image tool runner.
// It centralizes path resolution and sandbox policy:
//   - workBaseDir: base for resolving relative paths
//   - allowedRoots: optional restriction; if empty/nil, allow all
//   - blockSymlinks: blocks symlink traversal (if enforced downstream).
type ImageTool struct {
	mu         sync.RWMutex
	toolPolicy *imageToolPolicy
}

type ImageToolOption func(*ImageTool) error

func WithAllowedRoots(roots []string) ImageToolOption {
	return func(it *ImageTool) error {
		if it.toolPolicy == nil {
			it.toolPolicy = &imageToolPolicy{}
		}
		it.toolPolicy.allowedRoots = roots
		return nil
	}
}

func WithWorkBaseDir(base string) ImageToolOption {
	return func(it *ImageTool) error {
		if it.toolPolicy == nil {
			it.toolPolicy = &imageToolPolicy{}
		}
		it.toolPolicy.workBaseDir = base
		return nil
	}
}

// WithBlockSymlinks configures whether symlink traversal should be blocked (if supported downstream).
func WithBlockSymlinks(block bool) ImageToolOption {
	return func(it *ImageTool) error {
		if it.toolPolicy == nil {
			it.toolPolicy = &imageToolPolicy{}
		}
		it.toolPolicy.blockSymlinks = block
		return nil
	}
}

func NewImageTool(opts ...ImageToolOption) (*ImageTool, error) {
	it := &ImageTool{
		toolPolicy: &imageToolPolicy{
			allowedRoots:  nil,
			workBaseDir:   "",
			blockSymlinks: false,
		},
	}

	for _, opt := range opts {
		if opt == nil {
			continue
		}
		if err := opt(it); err != nil {
			return nil, err
		}
	}

	eff, roots, err := fileutil.InitPathPolicy(it.toolPolicy.workBaseDir, it.toolPolicy.allowedRoots)
	if err != nil {
		return nil, err
	}

	it.toolPolicy = &imageToolPolicy{
		allowedRoots:  roots,
		workBaseDir:   eff,
		blockSymlinks: it.toolPolicy.blockSymlinks,
	}

	return it, nil
}

func (it *ImageTool) ReadImageTool() spec.Tool { return toolutil.CloneTool(readImageTool) }

func (it *ImageTool) ReadImage(ctx context.Context, args ReadImageArgs) (*ReadImageOut, error) {
	return toolutil.WithRecoveryResp(func() (*ReadImageOut, error) {
		p := it.snapshotPolicy()
		return readImage(ctx, args, *p)
	})
}

func (it *ImageTool) snapshotPolicy() *imageToolPolicy {
	it.mu.RLock()
	p := it.toolPolicy
	it.mu.RUnlock()
	if p == nil {
		return nil
	}
	return p.Clone()
}
