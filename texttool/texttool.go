package texttool

import (
	"context"
	"slices"
	"sync"

	"github.com/flexigpt/llmtools-go/internal/fileutil"
	"github.com/flexigpt/llmtools-go/internal/toolutil"
	"github.com/flexigpt/llmtools-go/spec"
)

// TextTool is an instance-owned text tool runner.
// It centralizes path resolution and sandbox policy:
//   - workBaseDir: base for resolving relative paths
//   - allowedRoots: optional restriction; if empty/nil, allow all
type TextTool struct {
	mu           sync.RWMutex
	allowedRoots []string
	workBaseDir  string
}

type TextToolOption func(*TextTool) error

func WithAllowedRoots(roots []string) TextToolOption {
	return func(tt *TextTool) error {
		tt.allowedRoots = roots
		return nil
	}
}

func WithWorkBaseDir(base string) TextToolOption {
	return func(tt *TextTool) error {
		tt.workBaseDir = base
		return nil
	}
}

func NewTextTool(opts ...TextToolOption) (*TextTool, error) {
	tt := &TextTool{
		allowedRoots: nil,
		workBaseDir:  "",
	}
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		if err := opt(tt); err != nil {
			return nil, err
		}
	}

	eff, roots, err := fileutil.InitPathPolicy(tt.workBaseDir, tt.allowedRoots)
	if err != nil {
		return nil, err
	}
	tt.workBaseDir = eff
	tt.allowedRoots = roots
	return tt, nil
}

func (tt *TextTool) Tools() []spec.Tool {
	return []spec.Tool{
		tt.DeleteTextLinesTool(),
		tt.FindTextTool(),
		tt.InsertTextLinesTool(),
		tt.ReadTextRangeTool(),
		tt.ReplaceTextLinesTool(),
	}
}

func (tt *TextTool) DeleteTextLinesTool() spec.Tool { return toolutil.CloneTool(deleteTextLinesTool) }
func (tt *TextTool) FindTextTool() spec.Tool        { return toolutil.CloneTool(findTextTool) }
func (tt *TextTool) InsertTextLinesTool() spec.Tool { return toolutil.CloneTool(insertTextLinesTool) }
func (tt *TextTool) ReadTextRangeTool() spec.Tool   { return toolutil.CloneTool(readTextRangeTool) }
func (tt *TextTool) ReplaceTextLinesTool() spec.Tool {
	return toolutil.CloneTool(replaceTextLinesTool)
}

func (tt *TextTool) DeleteTextLines(ctx context.Context, args DeleteTextLinesArgs) (*DeleteTextLinesOut, error) {
	return toolutil.WithRecoveryResp(func() (*DeleteTextLinesOut, error) {
		base, roots := tt.snapshotPolicy()
		return deleteTextLines(ctx, args, base, roots)
	})
}

func (tt *TextTool) FindText(ctx context.Context, args FindTextArgs) (*FindTextOut, error) {
	return toolutil.WithRecoveryResp(func() (*FindTextOut, error) {
		base, roots := tt.snapshotPolicy()
		return findText(ctx, args, base, roots)
	})
}

func (tt *TextTool) InsertTextLines(ctx context.Context, args InsertTextLinesArgs) (*InsertTextLinesOut, error) {
	return toolutil.WithRecoveryResp(func() (*InsertTextLinesOut, error) {
		base, roots := tt.snapshotPolicy()
		return insertTextLines(ctx, args, base, roots)
	})
}

func (tt *TextTool) ReadTextRange(ctx context.Context, args ReadTextRangeArgs) (*ReadTextRangeOut, error) {
	return toolutil.WithRecoveryResp(func() (*ReadTextRangeOut, error) {
		base, roots := tt.snapshotPolicy()
		return readTextRange(ctx, args, base, roots)
	})
}

func (tt *TextTool) ReplaceTextLines(ctx context.Context, args ReplaceTextLinesArgs) (*ReplaceTextLinesOut, error) {
	return toolutil.WithRecoveryResp(func() (*ReplaceTextLinesOut, error) {
		base, roots := tt.snapshotPolicy()
		return replaceTextLines(ctx, args, base, roots)
	})
}

func (tt *TextTool) snapshotPolicy() (base string, roots []string) {
	tt.mu.RLock()
	base = tt.workBaseDir
	roots = slices.Clone(tt.allowedRoots)
	tt.mu.RUnlock()
	return base, roots
}
