package fs

import (
	"context"

	"github.com/ppipada/llmtools-go/internal/fileutil"
	"github.com/ppipada/llmtools-go/spec"
)

const SearchFilesFuncID spec.FuncID = "github.com/ppipada/llmtools-go/fs/searchfiles.SearchFiles"

var SearchFilesTool = spec.Tool{
	SchemaVersion: spec.SchemaVersion,
	ID:            "018fe0f4-b8cd-7e55-82d5-9df0bd70e4bc",
	Slug:          "searchfiles",
	Version:       "v1.0.0",
	DisplayName:   "Search files (content or path)",
	Description:   "Recursively search files whose name or textual content matches a regular expression.",
	Tags:          []string{"fs", "search"},

	ArgSchema: spec.JSONSchema(`{
		"$schema": "http://json-schema.org/draft-07/schema#",
		"type": "object",
		"properties": {
			"root": {
				"type": "string",
				"description": "Directory to start searching from.",
				"default": "."
			},
			"pattern": {
				"type": "string",
				"description": "RE2 regular expression applied to file path and file content."
			},
			"maxResults": {
				"type": "integer",
				"description": "Stop after this many matches (0 = unlimited).",
				"default": 100
			}
		},
		"required": ["pattern"],
		"additionalProperties": false
	}`),
	GoImpl: spec.GoToolImpl{FuncID: SearchFilesFuncID},

	CreatedAt:  spec.SchemaStartTime,
	ModifiedAt: spec.SchemaStartTime,
}

type SearchFilesArgs struct {
	Root       string `json:"root,omitempty"` // default "."
	Pattern    string `json:"pattern"`        // required (RE2)
	MaxResults int    `json:"maxResults,omitempty"`
}
type SearchFilesOut struct {
	Matches []string `json:"matches"`
}

// SearchFiles walks Root (recursively) and returns up to MaxResults files
// whose *path* or *UTF-8 text content* match the supplied regexp.
func SearchFiles(_ context.Context, args SearchFilesArgs) (*SearchFilesOut, error) {
	matches, err := fileutil.SearchFiles(args.Root, args.Pattern, args.MaxResults)
	if err != nil {
		return nil, err
	}
	return &SearchFilesOut{Matches: matches}, nil
}
