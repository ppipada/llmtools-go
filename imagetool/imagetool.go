package imagetool

import (
	"context"
	"slices"
	"sync"

	"github.com/flexigpt/llmtools-go/internal/fileutil"
	"github.com/flexigpt/llmtools-go/internal/toolutil"
	"github.com/flexigpt/llmtools-go/spec"
)

// ImageTool is an instance-owned image tool runner.
// It centralizes path resolution and sandbox policy:
//   - workBaseDir: base for resolving relative paths
//   - allowedRoots: optional restriction; if empty/nil, allow all
type ImageTool struct {
	mu           sync.RWMutex
	allowedRoots []string
	workBaseDir  string
}

type ImageToolOption func(*ImageTool) error

func WithAllowedRoots(roots []string) ImageToolOption {
	return func(it *ImageTool) error {
		it.allowedRoots = roots
		return nil
	}
}

func WithWorkBaseDir(base string) ImageToolOption {
	return func(it *ImageTool) error {
		it.workBaseDir = base
		return nil
	}
}

func NewImageTool(opts ...ImageToolOption) (*ImageTool, error) {
	it := &ImageTool{
		allowedRoots: nil,
		workBaseDir:  "",
	}
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		if err := opt(it); err != nil {
			return nil, err
		}
	}

	eff, roots, err := fileutil.InitPathPolicy(it.workBaseDir, it.allowedRoots)
	if err != nil {
		return nil, err
	}
	it.workBaseDir = eff
	it.allowedRoots = roots
	return it, nil
}

func (it *ImageTool) Tools() []spec.Tool {
	return []spec.Tool{
		it.ReadImageTool(),
	}
}

func (it *ImageTool) ReadImageTool() spec.Tool {
	return toolutil.CloneTool(readImageTool)
}

func (it *ImageTool) ReadImage(ctx context.Context, args ReadImageArgs) (*ReadImageOut, error) {
	return toolutil.WithRecoveryResp(func() (*ReadImageOut, error) {
		base, roots := it.snapshotPolicy()
		return readImage(ctx, args, base, roots)
	})
}

func (it *ImageTool) snapshotPolicy() (base string, roots []string) {
	it.mu.RLock()
	base = it.workBaseDir
	roots = slices.Clone(it.allowedRoots)
	it.mu.RUnlock()
	return base, roots
}
