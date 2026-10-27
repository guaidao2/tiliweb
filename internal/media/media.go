// Package media validates and stores uploaded images.
package media

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/disintegration/imaging"
	_ "golang.org/x/image/webp"

	"tiliweb/internal/config"
	"tiliweb/internal/content"
)

var allowed = map[string]string{
	"image/jpeg": ".jpg",
	"image/png":  ".png",
	"image/gif":  ".gif",
	"image/webp": ".webp",
}

func SaveImage(ctx context.Context, cfg *config.Config, store *content.Store, r io.Reader, originalName, uploaderType string, uploaderID int64) (*content.Media, error) {
	limit := cfg.Uploads.MaxImageBytes
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("empty upload")
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("image exceeds %d bytes", limit)
	}
	mime := http.DetectContentType(data)
	ext, ok := allowed[mime]
	if !ok {
		return nil, fmt.Errorf("unsupported image type %s", mime)
	}
	img, err := imaging.Decode(bytes.NewReader(data), imaging.AutoOrientation(true))
	if err != nil {
		return nil, fmt.Errorf("decode image: %w", err)
	}
	b := img.Bounds()
	if int64(b.Dx())*int64(b.Dy()) > cfg.Uploads.ImageMaxPixels {
		return nil, fmt.Errorf("image too large")
	}
	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])
	relDir := time.Now().UTC().Format("2006/01")
	absDir := filepath.Join(cfg.Storage.MediaDir, relDir)
	if err := os.MkdirAll(absDir, 0o750); err != nil {
		return nil, err
	}
	base := hash[:16]
	stored := filepath.Join(relDir, base+ext)
	if err := os.WriteFile(filepath.Join(cfg.Storage.MediaDir, stored), data, 0o640); err != nil {
		return nil, err
	}

	var variants []content.MediaVariant
	if mime != "image/gif" {
		for _, w := range []int{320, 768, 1280} {
			if b.Dx() <= w {
				continue
			}
			resized := imaging.Resize(img, w, 0, imaging.Lanczos)
			vname := filepath.Join(relDir, fmt.Sprintf("%s-%d.jpg", base, w))
			f, err := os.Create(filepath.Join(cfg.Storage.MediaDir, vname))
			if err != nil {
				return nil, err
			}
			err = imaging.Encode(f, resized, imaging.JPEG, imaging.JPEGQuality(85))
			_ = f.Close()
			if err != nil {
				return nil, err
			}
			st, statErr := os.Stat(filepath.Join(cfg.Storage.MediaDir, vname))
			if statErr != nil {
				return nil, statErr
			}
			variants = append(variants, content.MediaVariant{
				Label: fmt.Sprintf("%d", w), StoredName: vname,
				Width: resized.Bounds().Dx(), Height: resized.Bounds().Dy(), Size: st.Size(),
			})
		}
	}

	return store.CreateMedia(ctx, &content.Media{
		Kind: "image", StoredName: stored, OriginalName: filepath.Base(originalName),
		MIME: mime, Size: int64(len(data)), Width: b.Dx(), Height: b.Dy(),
		SHA256: hash, UploaderType: uploaderType, UploaderID: uploaderID,
	}, variants)
}

func Path(mediaDir, storedName string) (string, bool) {
	clean := filepath.Clean(storedName)
	if strings.HasPrefix(clean, "..") || filepath.IsAbs(clean) {
		return "", false
	}
	full := filepath.Join(filepath.Clean(mediaDir), clean)
	base := filepath.Clean(mediaDir) + string(filepath.Separator)
	if !strings.HasPrefix(full, base) {
		return "", false
	}
	return full, true
}

var documentTypes = map[string]string{
	".pdf":  "application/pdf",
	".zip":  "application/zip",
	".docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
	".xlsx": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
	".pptx": "application/vnd.openxmlformats-officedocument.presentationml.presentation",
	".txt":  "text/plain; charset=utf-8",
}

// SaveDocument validates and stores a downloadable attachment. Office files and
// zip archives must start with the ZIP magic; PDFs must start with %PDF-.
func SaveDocument(ctx context.Context, cfg *config.Config, store *content.Store, r io.Reader, originalName, uploaderType string, uploaderID int64) (*content.Media, error) {
	ext := strings.ToLower(filepath.Ext(originalName))
	mime, ok := documentTypes[ext]
	if !ok {
		return nil, fmt.Errorf("unsupported document type %s", ext)
	}
	limit := cfg.Uploads.MaxDocumentBytes
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("empty upload")
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("document exceeds %d bytes", limit)
	}
	switch ext {
	case ".pdf":
		if !bytes.HasPrefix(data, []byte("%PDF-")) {
			return nil, fmt.Errorf("invalid PDF header")
		}
	case ".zip", ".docx", ".xlsx", ".pptx":
		if !bytes.HasPrefix(data, []byte("PK\x03\x04")) {
			return nil, fmt.Errorf("invalid ZIP/Office header")
		}
	case ".txt":
		if bytes.IndexByte(data, 0) >= 0 {
			return nil, fmt.Errorf("invalid text document")
		}
	}
	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])
	relDir := time.Now().UTC().Format("2006/01")
	absDir := filepath.Join(cfg.Storage.DocumentDir, relDir)
	if err := os.MkdirAll(absDir, 0o750); err != nil {
		return nil, err
	}
	stored := filepath.Join(relDir, hash[:16]+ext)
	if err := os.WriteFile(filepath.Join(cfg.Storage.DocumentDir, stored), data, 0o640); err != nil {
		return nil, err
	}
	return store.CreateMedia(ctx, &content.Media{
		Kind: "document", StoredName: stored, OriginalName: filepath.Base(originalName),
		MIME: mime, Size: int64(len(data)), SHA256: hash, UploaderType: uploaderType, UploaderID: uploaderID,
	}, nil)
}
