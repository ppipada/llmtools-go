package texttool

import (
	"context"
	"sync"

	"github.com/flexigpt/llmtools-go/internal/fileutil"
	"github.com/flexigpt/llmtools-go/internal/toolutil"
	"github.com/flexigpt/llmtools-go/spec"
)

// textToolPolicy centralizes sandbox/path policy for this tool instance.
type textToolPolicy struct {
	allowedRoots  []string
	workBaseDir   string
	blockSymlinks bool
}

// Clone returns an independent copy of the policy.
func (p *textToolPolicy) Clone() *textToolPolicy {
	if p == nil {
		return nil
	}
	cp := *p
	cp.allowedRoots = append([]string(nil), p.allowedRoots...)
	return &cp
}

// TextTool is an instance-owned text tool runner.
// It centralizes path resolution and sandbox policy:
//   - workBaseDir: base for resolving relative paths
//   - allowedRoots: optional restriction; if empty/nil, allow all
//   - blockSymlinks: blocks symlink traversal (if enforced downstream).
type TextTool struct {
	mu         sync.RWMutex
	toolPolicy *textToolPolicy
}

type TextToolOption func(*TextTool) error

func WithAllowedRoots(roots []string) TextToolOption {
	return func(tt *TextTool) error {
		if tt.toolPolicy == nil {
			tt.toolPolicy = &textToolPolicy{}
		}
		tt.toolPolicy.allowedRoots = roots
		return nil
	}
}

func WithWorkBaseDir(base string) TextToolOption {
	return func(tt *TextTool) error {
		if tt.toolPolicy == nil {
			tt.toolPolicy = &textToolPolicy{}
		}
		tt.toolPolicy.workBaseDir = base
		return nil
	}
}

// WithBlockSymlinks configures whether symlink traversal should be blocked (if supported downstream).
func WithBlockSymlinks(block bool) TextToolOption {
	return func(tt *TextTool) error {
		if tt.toolPolicy == nil {
			tt.toolPolicy = &textToolPolicy{}
		}
		tt.toolPolicy.blockSymlinks = block
		return nil
	}
}

func NewTextTool(opts ...TextToolOption) (*TextTool, error) {
	tt := &TextTool{
		toolPolicy: &textToolPolicy{
			allowedRoots:  nil,
			workBaseDir:   "",
			blockSymlinks: false,
		},
	}

	for _, opt := range opts {
		if opt == nil {
			continue
		}
		if err := opt(tt); err != nil {
			return nil, err
		}
	}

	eff, roots, err := fileutil.InitPathPolicy(tt.toolPolicy.workBaseDir, tt.toolPolicy.allowedRoots)
	if err != nil {
		return nil, err
	}

	tt.toolPolicy = &textToolPolicy{
		allowedRoots:  roots,
		workBaseDir:   eff,
		blockSymlinks: tt.toolPolicy.blockSymlinks,
	}

	return tt, nil
}

func (tt *TextTool) DeleteTextLinesTool() spec.Tool  { return toolutil.CloneTool(deleteTextLinesTool) }
func (tt *TextTool) FindTextTool() spec.Tool         { return toolutil.CloneTool(findTextTool) }
func (tt *TextTool) InsertTextLinesTool() spec.Tool  { return toolutil.CloneTool(insertTextLinesTool) }
func (tt *TextTool) ReadTextRangeTool() spec.Tool    { return toolutil.CloneTool(readTextRangeTool) }
func (tt *TextTool) ReplaceTextLinesTool() spec.Tool { return toolutil.CloneTool(replaceTextLinesTool) }

func (tt *TextTool) DeleteTextLines(ctx context.Context, args DeleteTextLinesArgs) (*DeleteTextLinesOut, error) {
	return toolutil.WithRecoveryResp(func() (*DeleteTextLinesOut, error) {
		p := tt.snapshotPolicy()
		return deleteTextLines(ctx, args, *p)
	})
}

func (tt *TextTool) FindText(ctx context.Context, args FindTextArgs) (*FindTextOut, error) {
	return toolutil.WithRecoveryResp(func() (*FindTextOut, error) {
		p := tt.snapshotPolicy()
		return findText(ctx, args, *p)
	})
}

func (tt *TextTool) InsertTextLines(ctx context.Context, args InsertTextLinesArgs) (*InsertTextLinesOut, error) {
	return toolutil.WithRecoveryResp(func() (*InsertTextLinesOut, error) {
		p := tt.snapshotPolicy()
		return insertTextLines(ctx, args, *p)
	})
}

func (tt *TextTool) ReadTextRange(ctx context.Context, args ReadTextRangeArgs) (*ReadTextRangeOut, error) {
	return toolutil.WithRecoveryResp(func() (*ReadTextRangeOut, error) {
		p := tt.snapshotPolicy()
		return readTextRange(ctx, args, *p)
	})
}

func (tt *TextTool) ReplaceTextLines(ctx context.Context, args ReplaceTextLinesArgs) (*ReplaceTextLinesOut, error) {
	return toolutil.WithRecoveryResp(func() (*ReplaceTextLinesOut, error) {
		p := tt.snapshotPolicy()
		return replaceTextLines(ctx, args, *p)
	})
}

func (tt *TextTool) snapshotPolicy() *textToolPolicy {
	tt.mu.RLock()
	p := tt.toolPolicy
	tt.mu.RUnlock()
	if p == nil {
		return nil
	}
	return p.Clone()
}
