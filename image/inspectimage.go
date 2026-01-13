package image

import (
	"context"
	"time"

	"github.com/ppipada/llmtools-go/internal/fileutil"
	"github.com/ppipada/llmtools-go/spec"
)

const InspectImageFuncID spec.FuncID = "github.com/ppipada/llmtools-go/image/inspectimage.InspectImage"

var InspectImageTool = spec.Tool{
	SchemaVersion: spec.SchemaVersion,
	ID:            "018fe0f4-b8cd-7e55-82d5-9df0bd70e4be",
	Slug:          "inspectimage",
	Version:       "v1.0.0",
	DisplayName:   "Inspect image",
	Description:   "Return intrinsic metadata (dimensions, format, timestamps) for a local image file.",
	Tags:          []string{"image"},

	ArgSchema: spec.JSONSchema(`{
		"$schema": "http://json-schema.org/draft-07/schema#",
		"type": "object",
		"properties": {
			"path": {
				"type": "string",
				"description": "Absolute or relative path of the image to inspect."
			}
		},
		"required": ["path"],
		"additionalProperties": false
	}`),
	GoImpl: spec.GoToolImpl{FuncID: InspectImageFuncID},

	CreatedAt:  spec.SchemaStartTime,
	ModifiedAt: spec.SchemaStartTime,
}

type InspectImageArgs struct {
	Path string `json:"path"`
}

type InspectImageOut struct {
	Exists    bool       `json:"exists"`
	Width     int        `json:"width,omitempty"`
	Height    int        `json:"height,omitempty"`
	Format    string     `json:"format,omitempty"`
	SizeBytes int64      `json:"sizeBytes,omitempty"`
	ModTime   *time.Time `json:"modTime,omitempty"`
}

// InspectImage inspects an image file and returns its intrinsic metadata.
func InspectImage(ctx context.Context, args InspectImageArgs) (*InspectImageOut, error) {
	info, err := fileutil.ReadImage(args.Path, false)
	if err != nil {
		return nil, err
	}
	return &InspectImageOut{
		Exists:    info.Exists,
		Width:     info.Width,
		Height:    info.Height,
		Format:    info.Format,
		SizeBytes: info.Size,
		ModTime:   info.ModTime,
	}, nil
}
