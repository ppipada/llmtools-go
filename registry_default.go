package llmtools

import (
	"fmt"
	"time"

	"github.com/ppipada/llmtools-go/fs"
	"github.com/ppipada/llmtools-go/image"
)

// DefaultRegistry is a package-level global registry with a 5s timeout.
// It is created during package initialization and panics on failure.
var DefaultRegistry *Registry

func init() {
	DefaultRegistry = mustNewRegistry(WithCallTimeoutForAll(5 * time.Second))

	if err := RegisterOutputsTool(DefaultRegistry, fs.ReadFileTool, fs.ReadFile); err != nil {
		panic(err)
	}
	if err := RegisterTypedAsTextTool(DefaultRegistry, fs.ListDirectoryTool, fs.ListDirectory); err != nil {
		panic(err)
	}
	if err := RegisterTypedAsTextTool(DefaultRegistry, fs.SearchFilesTool, fs.SearchFiles); err != nil {
		panic(err)
	}
	if err := RegisterTypedAsTextTool(DefaultRegistry, fs.StatPathTool, fs.StatPath); err != nil {
		panic(err)
	}
	if err := RegisterTypedAsTextTool(DefaultRegistry, image.InspectImageTool, image.InspectImage); err != nil {
		panic(err)
	}
}

// mustNewRegistry panics if NewRegistry fails.
// This is useful for package-level initialization.
func mustNewRegistry(opts ...RegistryOption) *Registry {
	r, err := NewRegistry(opts...)
	if err != nil {
		panic(fmt.Errorf("localregistry: failed to create registry: %w", err))
	}
	return r
}
