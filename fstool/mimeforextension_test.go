package fstool

import (
	"context"
	"errors"
	"testing"
)

func TestMIMEForExtension(t *testing.T) {
	t.Parallel()

	type tc struct {
		name string
		ctx  func(t *testing.T) context.Context
		args MIMEForExtensionArgs

		wantErr           bool
		wantErrIsCanceled bool
		wantErrMsg        string

		wantExt     string
		wantNormExt string

		wantMIME  string
		wantBase  string
		wantMode  MIMEMode
		wantKnown bool
	}

	tests := []tc{
		{
			name: "context_canceled",
			ctx: func(t *testing.T) context.Context {
				t.Helper()
				ctx, cancel := context.WithCancel(t.Context())
				cancel()
				return ctx
			},
			args:              MIMEForExtensionArgs{Extension: "png"},
			wantErr:           true,
			wantErrIsCanceled: true,
		},
		{
			name: "invalid_extension_empty",

			args:       MIMEForExtensionArgs{Extension: "   "},
			wantErr:    true,
			wantErrMsg: "invalid path",
		},
		{
			name:        "known_png_dot",
			args:        MIMEForExtensionArgs{Extension: ".png"},
			wantErr:     false,
			wantExt:     ".png",
			wantNormExt: ".png",
			wantMIME:    "image/png",
			wantBase:    "image/png",
			wantMode:    MIMEModeImage,
			wantKnown:   true,
		},
		{
			name:        "known_png_no_dot_uppercase",
			args:        MIMEForExtensionArgs{Extension: "PNG"},
			wantErr:     false,
			wantExt:     "PNG",
			wantNormExt: ".png",
			wantMIME:    "image/png",
			wantBase:    "image/png",
			wantMode:    MIMEModeImage,
			wantKnown:   true,
		},
		{
			name:        "known_txt_includes_charset_base_stripped",
			args:        MIMEForExtensionArgs{Extension: ".txt"},
			wantErr:     false,
			wantExt:     ".txt",
			wantNormExt: ".txt",
			wantMIME:    "text/plain; charset=utf-8",
			wantBase:    "text/plain",
			wantMode:    MIMEModeText,
			wantKnown:   true,
		},
		{
			name:        "known_yaml_yml_normalizes",
			args:        MIMEForExtensionArgs{Extension: "yMl"},
			wantErr:     false,
			wantExt:     "yMl",
			wantNormExt: ".yml",
			wantMIME:    "application/x-yaml",
			wantBase:    "application/x-yaml",
			wantMode:    MIMEModeText,
			wantKnown:   true,
		},
		{
			name:        "unknown_extension_returns_octet_stream_no_error",
			args:        MIMEForExtensionArgs{Extension: ".unknownext"},
			wantErr:     false,
			wantExt:     ".unknownext",
			wantNormExt: ".unknownext",
			wantMIME:    "application/octet-stream",
			wantBase:    "application/octet-stream",
			wantMode:    MIMEModeDefault,
			wantKnown:   false,
		},
		{
			name: "weird_extension_just_dot_treated_unknown",

			args:        MIMEForExtensionArgs{Extension: "."},
			wantErr:     false,
			wantExt:     ".",
			wantNormExt: ".",
			wantMIME:    "application/octet-stream",
			wantBase:    "application/octet-stream",
			wantMode:    MIMEModeDefault,
			wantKnown:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			if tt.ctx != nil {
				ctx = tt.ctx(t)
			}

			out, err := mimeForExtension(ctx, tt.args)

			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil (out=%+v)", out)
				}
				if tt.wantErrIsCanceled && !errors.Is(err, context.Canceled) {
					t.Fatalf("expected context.Canceled, got: %v", err)
				}
				if tt.wantErrMsg != "" && err.Error() != tt.wantErrMsg {
					t.Fatalf("expected err msg %q, got %q", tt.wantErrMsg, err.Error())
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if out == nil {
				t.Fatalf("expected non-nil out")
			}

			if out.Extension != tt.wantExt {
				t.Fatalf("Extension: got %q want %q", out.Extension, tt.wantExt)
			}
			if out.NormalizedExtension != tt.wantNormExt {
				t.Fatalf("NormalizedExtension: got %q want %q", out.NormalizedExtension, tt.wantNormExt)
			}
			if out.MIMEType != tt.wantMIME {
				t.Fatalf("MIMEType: got %q want %q", out.MIMEType, tt.wantMIME)
			}
			if out.BaseMIMEType != tt.wantBase {
				t.Fatalf("BaseMIMEType: got %q want %q", out.BaseMIMEType, tt.wantBase)
			}
			if out.Mode != tt.wantMode {
				t.Fatalf("Mode: got %q want %q", out.Mode, tt.wantMode)
			}
			if out.Known != tt.wantKnown {
				t.Fatalf("Known: got %v want %v", out.Known, tt.wantKnown)
			}
		})
	}
}
