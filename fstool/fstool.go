package fstool

import (
	"context"
	"os"
	"slices"
	"strings"
	"sync"

	"github.com/flexigpt/llmtools-go/internal/fileutil"
	"github.com/flexigpt/llmtools-go/internal/toolutil"
	"github.com/flexigpt/llmtools-go/spec"
)

// FSTool is an instance-owned filesystem tool runner.
// It centralizes path resolution and sandbox policy:
//   - workBaseDir: base for resolving relative paths
//   - allowedRoots: optional restriction; if empty, allow all
type FSTool struct {
	mu           sync.RWMutex
	allowedRoots []string
	workBaseDir  string
}

type FSToolOption func(*FSTool) error

// WithAllowedRoots restricts all filesystem paths to be within one of the provided roots.
// Roots are canonicalized (clean+abs+best-effort symlink eval) and must exist as directories.
func WithAllowedRoots(roots []string) FSToolOption {
	return func(ft *FSTool) error {
		ft.allowedRoots = roots
		return nil
	}
}

// WithWorkBaseDir sets the base directory used to resolve relative input paths.
// If empty/whitespace, the current process working directory is used.
func WithWorkBaseDir(base string) FSToolOption {
	return func(ft *FSTool) error {
		ft.workBaseDir = base
		return nil
	}
}

func NewFSTool(opts ...FSToolOption) (*FSTool, error) {
	ft := &FSTool{
		allowedRoots: nil,
		workBaseDir:  "",
	}
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		if err := opt(ft); err != nil {
			return nil, err
		}
	}

	eff, roots, err := fileutil.InitPathPolicy(ft.workBaseDir, ft.allowedRoots)
	if err != nil {
		return nil, err
	}

	ft.workBaseDir = eff
	ft.allowedRoots = roots

	return ft, nil
}

func (ft *FSTool) WorkBaseDir() string {
	ft.mu.RLock()
	defer ft.mu.RUnlock()
	return ft.workBaseDir
}

func (ft *FSTool) AllowedRoots() []string {
	ft.mu.RLock()
	defer ft.mu.RUnlock()
	return append([]string(nil), ft.allowedRoots...)
}

// SetAllowedRoots updates allowed roots at runtime (best-effort).
// If the current workBaseDir is not within the new roots, this returns an error and leaves state unchanged.
func (ft *FSTool) SetAllowedRoots(roots []string) error {
	canon, err := fileutil.CanonicalizeAllowedRoots(roots)
	if err != nil {
		return err
	}
	ft.mu.Lock()
	defer ft.mu.Unlock()
	if _, err := fileutil.GetEffectiveWorkDir(ft.workBaseDir, canon); err != nil {
		return err
	}
	ft.allowedRoots = canon
	return nil
}

// SetWorkBaseDir updates the work base directory at runtime (best-effort).
func (ft *FSTool) SetWorkBaseDir(base string) error {
	ft.mu.RLock()
	roots := append([]string(nil), ft.allowedRoots...)
	ft.mu.RUnlock()

	b := strings.TrimSpace(base)
	if b == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		b = cwd
	}
	eff, err := fileutil.GetEffectiveWorkDir(b, roots)
	if err != nil {
		return err
	}

	ft.mu.Lock()
	ft.workBaseDir = eff
	ft.mu.Unlock()
	return nil
}

// Tools returns all fstool tool specs for registration.
func (ft *FSTool) Tools() []spec.Tool {
	return []spec.Tool{
		ft.ReadFileTool(),
		ft.WriteFileTool(),
		ft.DeleteFileTool(),
		ft.ListDirectoryTool(),
		ft.SearchFilesTool(),
		ft.StatPathTool(),
		ft.MIMEForPathTool(),
		ft.MIMEForExtensionTool(),
	}
}

func (ft *FSTool) DeleteFileTool() spec.Tool {
	return toolutil.CloneTool(deleteFileTool)
}

func (ft *FSTool) DeleteFile(ctx context.Context, args DeleteFileArgs) (*DeleteFileOut, error) {
	return toolutil.WithRecoveryResp(func() (*DeleteFileOut, error) {
		base, roots := ft.snapshotPolicy()
		return deleteFile(ctx, args, base, roots)
	})
}

func (ft *FSTool) ListDirectoryTool() spec.Tool {
	return toolutil.CloneTool(listDirectoryTool)
}

func (ft *FSTool) ListDirectory(ctx context.Context, args ListDirectoryArgs) (*ListDirectoryOut, error) {
	return toolutil.WithRecoveryResp(func() (*ListDirectoryOut, error) {
		base, roots := ft.snapshotPolicy()
		return listDirectory(ctx, args, base, roots)
	})
}

func (ft *FSTool) MIMEForExtensionTool() spec.Tool {
	return toolutil.CloneTool(mimeForExtensionTool)
}

func (ft *FSTool) MIMEForExtension(ctx context.Context, args MIMEForExtensionArgs) (*MIMEForExtensionOut, error) {
	return toolutil.WithRecoveryResp(func() (*MIMEForExtensionOut, error) {
		return mimeForExtension(ctx, args)
	})
}

func (ft *FSTool) MIMEForPathTool() spec.Tool {
	return toolutil.CloneTool(mimeForPathTool)
}

func (ft *FSTool) MIMEForPath(ctx context.Context, args MIMEForPathArgs) (*MIMEForPathOut, error) {
	return toolutil.WithRecoveryResp(func() (*MIMEForPathOut, error) {
		base, roots := ft.snapshotPolicy()
		return mimeForPath(ctx, args, base, roots)
	})
}

func (ft *FSTool) ReadFileTool() spec.Tool {
	return toolutil.CloneTool(readFileTool)
}

func (ft *FSTool) ReadFile(
	ctx context.Context,
	args ReadFileArgs,
) ([]spec.ToolStoreOutputUnion, error) {
	return toolutil.WithRecoveryResp(func() ([]spec.ToolStoreOutputUnion, error) {
		base, roots := ft.snapshotPolicy()
		return readFile(ctx, args, base, roots)
	})
}

func (ft *FSTool) SearchFilesTool() spec.Tool {
	return toolutil.CloneTool(searchFilesTool)
}

func (ft *FSTool) SearchFiles(ctx context.Context, args SearchFilesArgs) (*SearchFilesOut, error) {
	return toolutil.WithRecoveryResp(func() (*SearchFilesOut, error) {
		base, roots := ft.snapshotPolicy()
		return searchFiles(ctx, args, base, roots)
	})
}

func (ft *FSTool) StatPathTool() spec.Tool {
	return toolutil.CloneTool(statPathTool)
}

func (ft *FSTool) StatPath(ctx context.Context, args StatPathArgs) (*StatPathOut, error) {
	return toolutil.WithRecoveryResp(func() (*StatPathOut, error) {
		base, roots := ft.snapshotPolicy()
		return statPath(ctx, args, base, roots)
	})
}

func (ft *FSTool) WriteFileTool() spec.Tool {
	return toolutil.CloneTool(writeFileTool)
}

func (ft *FSTool) WriteFile(ctx context.Context, args WriteFileArgs) (*WriteFileOut, error) {
	return toolutil.WithRecoveryResp(func() (*WriteFileOut, error) {
		base, roots := ft.snapshotPolicy()
		return writeFile(ctx, args, base, roots)
	})
}

func (ft *FSTool) snapshotPolicy() (base string, roots []string) {
	ft.mu.RLock()
	base = ft.workBaseDir
	roots = slices.Clone(ft.allowedRoots)
	ft.mu.RUnlock()
	return base, roots
}
