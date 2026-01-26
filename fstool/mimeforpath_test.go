package fstool

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestMIMEForPath(t *testing.T) {
	t.Parallel()

	writeFile := func(t *testing.T, dir, name string, data []byte) string {
		t.Helper()
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, data, 0o600); err != nil {
			t.Fatalf("WriteFile(%q): %v", p, err)
		}
		return p
	}

	pngHeader := []byte{
		0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n',
		// Extra bytes to avoid edge conditions; DetectContentType only needs the signature.
		0x00, 0x00, 0x00, 0x0D, 'I', 'H', 'D', 'R',
	}

	binaryWithNUL := []byte{0x00, 0x01, 0x02, 0x03, 0x04}

	type tc struct {
		name string
		ctx  func(t *testing.T) context.Context
		args func(t *testing.T) MIMEForPathArgs

		wantErr           bool
		wantErrIsNotExist bool
		wantErrIsCanceled bool
		wantErrIsPathErr  bool

		wantExt     string
		wantNormExt string

		wantMIME   string
		wantBase   string
		wantMode   MIMEMode
		wantMethod MIMEDetectMethod
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
			args: func(t *testing.T) MIMEForPathArgs {
				t.Helper()
				return MIMEForPathArgs{Path: "whatever.txt"}
			},
			wantErr:           true,
			wantErrIsCanceled: true,
		},
		{
			name: "invalid_path_empty",

			args: func(t *testing.T) MIMEForPathArgs {
				t.Helper()
				return MIMEForPathArgs{Path: "   "}
			},
			wantErr: true,
		},
		{
			name: "nonexistent_known_extension_uses_extension_no_io",

			args: func(t *testing.T) MIMEForPathArgs {
				t.Helper()
				dir := t.TempDir()
				// Uppercase extension to ensure normalization behavior is correct.
				return MIMEForPathArgs{Path: filepath.Join(dir, "missing.PDF")}
			},
			wantErr:     false,
			wantExt:     ".PDF",
			wantNormExt: ".pdf",
			wantMIME:    "application/pdf",
			wantBase:    "application/pdf",
			wantMode:    MIMEModeDocument,
			wantMethod:  MIMEDetectMethodExtension,
		},
		{
			name: "nonexistent_unknown_extension_errors_not_exist",

			args: func(t *testing.T) MIMEForPathArgs {
				t.Helper()
				dir := t.TempDir()
				return MIMEForPathArgs{Path: filepath.Join(dir, "missing.unknownext")}
			},
			wantErr:           true,
			wantErrIsNotExist: true,
		},
		{
			name: "existing_txt_uses_extension",

			args: func(t *testing.T) MIMEForPathArgs {
				t.Helper()
				dir := t.TempDir()
				p := writeFile(t, dir, "a.txt", []byte("hello\n"))
				return MIMEForPathArgs{Path: p}
			},
			wantErr:     false,
			wantExt:     ".txt",
			wantNormExt: ".txt",
			wantMIME:    "text/plain; charset=utf-8",
			wantBase:    "text/plain",
			wantMode:    MIMEModeText,
			wantMethod:  MIMEDetectMethodExtension,
		},
		{
			name: "existing_no_extension_sniffs_text",

			args: func(t *testing.T) MIMEForPathArgs {
				t.Helper()
				dir := t.TempDir()
				p := writeFile(t, dir, "noext", []byte("hello\n"))
				return MIMEForPathArgs{Path: p}
			},
			wantErr:     false,
			wantExt:     "",
			wantNormExt: "",
			wantMIME:    "text/plain; charset=utf-8",
			wantBase:    "text/plain",
			wantMode:    MIMEModeText,
			wantMethod:  MIMEDetectMethodSniff,
		},
		{
			name: "existing_unknown_extension_sniffs_png",

			args: func(t *testing.T) MIMEForPathArgs {
				t.Helper()
				dir := t.TempDir()
				p := writeFile(t, dir, "x.bin", pngHeader)
				return MIMEForPathArgs{Path: p}
			},
			wantErr:     false,
			wantExt:     ".bin",
			wantNormExt: ".bin",
			wantMIME:    "image/png",
			wantBase:    "image/png",
			wantMode:    MIMEModeImage,
			wantMethod:  MIMEDetectMethodSniff,
		},
		{
			name: "existing_unknown_extension_sniffs_binary_octet_stream",

			args: func(t *testing.T) MIMEForPathArgs {
				t.Helper()
				dir := t.TempDir()
				p := writeFile(t, dir, "x.bin", binaryWithNUL)
				return MIMEForPathArgs{Path: p}
			},
			wantErr:     false,
			wantExt:     ".bin",
			wantNormExt: ".bin",
			wantMIME:    "application/octet-stream",
			wantBase:    "application/octet-stream",
			wantMode:    MIMEModeDefault,
			wantMethod:  MIMEDetectMethodSniff,
		},
		{
			name: "directory_path_errors",

			args: func(t *testing.T) MIMEForPathArgs {
				t.Helper()
				dir := t.TempDir()
				return MIMEForPathArgs{Path: dir}
			},
			wantErr:          true,
			wantErrIsPathErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			if tt.ctx != nil {
				ctx = tt.ctx(t)
			}

			out, err := MIMEForPath(ctx, tt.args(t))

			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil (out=%+v)", out)
				}

				if tt.wantErrIsCanceled && !errors.Is(err, context.Canceled) {
					t.Fatalf("expected context.Canceled, got: %v", err)
				}
				if tt.wantErrIsNotExist && !os.IsNotExist(err) {
					t.Fatalf("expected IsNotExist error, got: %T %v", err, err)
				}
				if tt.wantErrIsPathErr {
					var pe *os.PathError
					if !errors.As(err, &pe) {
						t.Fatalf("expected *os.PathError, got: %T %v", err, err)
					}
				}

				// For invalid path we can assert message without importing internal packages.
				if tt.name == "invalid_path_empty" && err.Error() != "invalid path" {
					t.Fatalf("expected error %q, got %q", "invalid path", err.Error())
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
			if out.Method != tt.wantMethod {
				t.Fatalf("Method: got %q want %q", out.Method, tt.wantMethod)
			}
		})
	}
}
